package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	apiBase   = "https://api.github.com"
	userAgent = "RepoDownloader/0.1"
)

type FileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// RepoTree is what the frontend receives. The json keys match what the UI
// expects (snake_case for git_ref / default_branch).
type RepoTree struct {
	Owner         string      `json:"owner"`
	Repo          string      `json:"repo"`
	GitRef        string      `json:"git_ref"`
	DefaultBranch string      `json:"default_branch"`
	Branches      []string    `json:"branches"`
	Files         []FileEntry `json:"files"`
	Truncated     bool        `json:"truncated"`
}

type repoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

type branchInfo struct {
	Name string `json:"name"`
}

type treeItem struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

type treeResponse struct {
	Tree      []treeItem `json:"tree"`
	Truncated bool       `json:"truncated"`
}

// parseRepo accepts "owner/repo", a full GitHub URL, "git@github.com:owner/repo.git"
// and returns the owner and repository name.
func parseRepo(input string) (string, string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", fmt.Errorf("请输入仓库，例如 owner/repo")
	}

	for _, prefix := range []string{"https://", "http://", "ssh://"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	s = strings.TrimPrefix(s, "git@")
	s = strings.ReplaceAll(s, ":", "/")
	for _, host := range []string{"github.com/", "www.github.com/"} {
		if strings.HasPrefix(s, host) {
			s = strings.TrimPrefix(s, host)
			break
		}
	}
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")

	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' })
	if len(parts) < 2 {
		return "", "", fmt.Errorf("无法从“%s”识别仓库，请输入 owner/repo 或完整的 GitHub 链接", input)
	}
	return parts[0], parts[1], nil
}

// escapeRef percent-encodes a ref name or a repo path for use inside a URL,
// deliberately keeping "/" readable — branch names and file paths may contain it.
func escapeRef(s string) string {
	const unreserved = "-._~"
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || strings.ContainsRune(unreserved, r):
			b.WriteRune(r)
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			for i := 0; i < len(string(r)); i++ {
				fmt.Fprintf(&b, "%%%02X", string(r)[i])
			}
		}
	}
	return b.String()
}

func humanizeAPIError(code int, body string) string {
	hint := "请求失败"
	switch code {
	case 401:
		hint = "认证失败，请检查 Token 是否正确"
	case 403:
		hint = "请求被拒绝：可能是速率限制，或凭据没有写权限（需要带 repo 权限）"
	case 404:
		hint = "仓库或分支不存在（私有仓库需要填写 Token）"
	case 409:
		hint = "冲突：分支已被其他人更新，请重新加载后再试"
	case 422:
		hint = "无法应用改动：分支名无效，或分支已被更新（不是快进），请重新加载后再试"
	case 429:
		hint = "请求过于频繁，请稍后再试"
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Sprintf("GitHub API %d：%s", code, hint)
	}
	return fmt.Sprintf("GitHub API %d：%s\n%s", code, hint, body)
}

func apiGet(ctx context.Context, client *http.Client, url, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("网络请求失败：%w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败：%w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s", humanizeAPIError(resp.StatusCode, string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析 GitHub 返回数据失败：%w", err)
	}
	return nil
}

// FetchRepoTree resolves the repository, lists its branches and walks the whole
// recursive tree of the requested ref. Authentication comes from the stored
// credentials (OAuth login or a manual token).
func (a *App) FetchRepoTree(repo string, gitRef string) (*RepoTree, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	client := newHTTPClient(60 * time.Second)
	token := a.effectiveToken()

	repoURL := fmt.Sprintf("%s/repos/%s/%s", apiBase, owner, name)

	var info repoInfo
	if err := apiGet(ctx, client, repoURL, token, &info); err != nil {
		return nil, err
	}
	defaultBranch := info.DefaultBranch
	if strings.TrimSpace(defaultBranch) == "" {
		defaultBranch = "main"
	}

	// The branch list is a nicety; failing to get it must not break loading.
	var branches []branchInfo
	if err := apiGet(ctx, client, repoURL+"/branches?per_page=100", token, &branches); err != nil {
		branches = nil
	}

	chosen := strings.TrimSpace(gitRef)
	if chosen == "" {
		chosen = defaultBranch
	}

	treeURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1",
		apiBase, owner, name, escapeRef(chosen))
	var tree treeResponse
	if err := apiGet(ctx, client, treeURL, token, &tree); err != nil {
		return nil, err
	}

	files := make([]FileEntry, 0, len(tree.Tree))
	for _, item := range tree.Tree {
		if item.Type != "blob" {
			continue
		}
		files = append(files, FileEntry{Path: item.Path, Size: item.Size})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	names := make([]string, 0, len(branches)+1)
	seen := false
	for _, b := range branches {
		if b.Name == defaultBranch {
			seen = true
		}
		names = append(names, b.Name)
	}
	if !seen {
		names = append([]string{defaultBranch}, names...)
	}

	return &RepoTree{
		Owner:         owner,
		Repo:          name,
		GitRef:        chosen,
		DefaultBranch: defaultBranch,
		Branches:      names,
		Files:         files,
		Truncated:     tree.Truncated,
	}, nil
}
