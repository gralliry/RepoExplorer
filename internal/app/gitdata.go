package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gralliry/RepoExplorer/internal/githubutil"
	"github.com/gralliry/RepoExplorer/internal/repopath"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// This file implements the write side of the GitHub API. Everything goes through
// the "Git Data" API rather than the Contents API, because that lets a whole
// operation (for example renaming a folder with 200 files in it, or uploading a
// folder) land as ONE atomic commit instead of hundreds of separate ones.

/* ------------------------------------------------------------------ types */

type treeEntryRequest struct {
	Path string  `json:"path"`
	Mode string  `json:"mode"`
	Type string  `json:"type"`
	SHA  *string `json:"sha"` // nil means "delete this path"
}

// contentSource says where the bytes of a new/updated file come from: either
// memory (the built-in editor) or a path on the local disk (upload).
type contentSource struct {
	inline []byte
	local  string
}

func (c *contentSource) bytes() ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("内部错误：缺少文件内容")
	}
	if c.inline != nil {
		return c.inline, nil
	}
	data, err := readLocalFile(c.local)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// treeChange is one path-level modification inside a commit.
type treeChange struct {
	Path    string
	Mode    string
	Delete  bool
	BlobSHA string         // set when reusing an existing blob (rename/move)
	Source  *contentSource // set when writing new content
}

// CommitResult is what the UI shows in a toast after a write.
type CommitResult struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Changes int    `json:"changes"`
	Commits int    `json:"commits"`
}

// PlannedChange describes one file an operation is about to touch. The UI uses
// these to build the "are you sure?" confirmation list.
type PlannedChange struct {
	Path   string `json:"path"`
	Action string `json:"action"` // "add" | "update" | "delete" | "move"
	From   string `json:"from,omitempty"`
	Size   int64  `json:"size"`
}

// defaultMessage returns the user supplied message, or generates one.
func defaultMessage(custom, format string, args ...any) string {
	if strings.TrimSpace(custom) != "" {
		return strings.TrimSpace(custom)
	}
	return fmt.Sprintf(format, args...)
}

// apiSend performs a JSON request against the GitHub API (POST / PATCH / PUT).
func apiSend(ctx context.Context, client *http.Client, method, url, token string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(data))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t := strings.TrimSpace(token); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("网络请求失败：%w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败：%w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s", humanizeAPIError(resp.StatusCode, string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析 GitHub 返回数据失败：%w", err)
	}
	return nil
}

/* ---------------------------------------------------------------- git data */

type refResponse struct {
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"object"`
}

type commitResponse struct {
	SHA  string `json:"sha"`
	Tree struct {
		SHA string `json:"sha"`
	} `json:"tree"`
}

type remoteEntry struct {
	SHA  string
	Mode string
	Size int64
}

// branchHead resolves a branch to its head commit and root tree.
func branchHead(ctx context.Context, client *http.Client, owner, repo, branch, token string) (string, string, error) {
	var ref refResponse
	url := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", apiBase, owner, repo, githubutil.EscapeRef(branch))
	if err := apiGet(ctx, client, url, token, &ref); err != nil {
		return "", "", err
	}
	if ref.Object.SHA == "" {
		return "", "", fmt.Errorf("分支 %s 不存在", branch)
	}

	var commit commitResponse
	url = fmt.Sprintf("%s/repos/%s/%s/git/commits/%s", apiBase, owner, repo, ref.Object.SHA)
	if err := apiGet(ctx, client, url, token, &commit); err != nil {
		return "", "", err
	}
	return commit.SHA, commit.Tree.SHA, nil
}

// treeIndex lists every blob in the repo at the given tree, keyed by path.
func treeIndex(ctx context.Context, client *http.Client, owner, repo, treeSHA, token string) (map[string]remoteEntry, error) {
	var out struct {
		Tree []struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Size int64  `json:"size"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", apiBase, owner, repo, treeSHA)
	if err := apiGet(ctx, client, url, token, &out); err != nil {
		return nil, err
	}
	if out.Truncated {
		return nil, fmt.Errorf("仓库太大，GitHub 截断了文件列表，写操作无法安全进行")
	}

	index := make(map[string]remoteEntry, len(out.Tree))
	for _, item := range out.Tree {
		if item.Type != "blob" {
			continue
		}
		index[item.Path] = remoteEntry{SHA: item.SHA, Mode: item.Mode, Size: item.Size}
	}
	return index, nil
}

func createBlob(ctx context.Context, client *http.Client, owner, repo, token string, content []byte) (string, error) {
	payload := map[string]string{
		"content":  base64.StdEncoding.EncodeToString(content),
		"encoding": "base64",
	}
	var out struct {
		SHA string `json:"sha"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/blobs", apiBase, owner, repo)
	if err := apiSend(ctx, client, http.MethodPost, url, token, payload, &out); err != nil {
		return "", err
	}
	if out.SHA == "" {
		return "", fmt.Errorf("GitHub 没有返回 blob 标识")
	}
	return out.SHA, nil
}

func readGitBlob(ctx context.Context, client *http.Client, owner, repo, token, sha string) ([]byte, error) {
	var blob struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/blobs/%s", apiBase, owner, repo, sha)
	if err := apiGet(ctx, client, url, token, &blob); err != nil {
		return nil, err
	}
	if blob.Encoding != "base64" {
		return nil, fmt.Errorf("不支持的 blob 编码：%s", blob.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("解码 blob 失败：%w", err)
	}
	return data, nil
}

func lfsPointer(oid string, size int64) []byte {
	return []byte(fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, size))
}

func uploadLFSObject(ctx context.Context, client *http.Client, owner, repo, token, localPath string) ([]byte, error) {
	data, err := readLocalFile(localPath)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	oid := fmt.Sprintf("%x", sum[:])
	size := int64(len(data))

	payload := struct {
		Operation string   `json:"operation"`
		Transfers []string `json:"transfers"`
		Objects   []struct {
			OID  string `json:"oid"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}{Operation: "upload", Transfers: []string{"basic"}}
	payload.Objects = append(payload.Objects, struct {
		OID  string `json:"oid"`
		Size int64  `json:"size"`
	}{OID: oid, Size: size})

	var out struct {
		Objects []struct {
			OID     string `json:"oid"`
			Size    int64  `json:"size"`
			Actions struct {
				Upload *struct {
					Href   string            `json:"href"`
					Header map[string]string `json:"header"`
				} `json:"upload"`
			} `json:"actions"`
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"objects"`
	}
	url := fmt.Sprintf("https://github.com/%s/%s.git/info/lfs/objects/batch", owner, repo)
	payloadData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.git-lfs+json")
	req.Header.Set("Content-Type", "application/vnd.git-lfs+json")
	req.Header.Set("User-Agent", userAgent)
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Git LFS 请求失败：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 Git LFS 响应失败：%w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("Git LFS 请求失败：%s", humanizeAPIError(resp.StatusCode, string(body)))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("解析 Git LFS 响应失败：%w", err)
	}
	if len(out.Objects) == 0 {
		return nil, fmt.Errorf("Git LFS 没有返回上传信息")
	}
	obj := out.Objects[0]
	if obj.Error != nil {
		return nil, fmt.Errorf("Git LFS 拒绝上传：%s", obj.Error.Message)
	}
	if obj.Actions.Upload != nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, obj.Actions.Upload.Href, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		for k, v := range obj.Actions.Upload.Header {
			req.Header.Set(k, v)
		}
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Git LFS 上传失败：%w", err)
		}
		defer resp.Body.Close()
		uploadBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("Git LFS 上传失败：%s", humanizeAPIError(resp.StatusCode, string(uploadBody)))
		}
	}
	return lfsPointer(oid, size), nil
}

func lfsAttributePattern(path string) string {
	path = strings.ReplaceAll(path, "\\", "\\\\")
	path = strings.ReplaceAll(path, " ", "\\ ")
	return path
}

func lfsAttributesContent(existing []byte, paths []string) ([]byte, bool) {
	lines := strings.Split(string(existing), "\n")
	hasPattern := func(pattern string) bool {
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == pattern || strings.HasPrefix(line, pattern+" ") || strings.HasPrefix(line, pattern+"\t") {
				return true
			}
		}
		return false
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteByte('\n')
	}
	changed := false
	for _, path := range paths {
		pattern := lfsAttributePattern(path)
		if hasPattern(pattern) {
			continue
		}
		b.WriteString(pattern)
		b.WriteString(" filter=lfs diff=lfs merge=lfs -text\n")
		lines = append(lines, pattern+" filter=lfs diff=lfs merge=lfs -text")
		changed = true
	}
	if !changed {
		return existing, false
	}
	return []byte(b.String()), true
}

func lfsAttributesChange(ctx context.Context, client *http.Client, owner, repo, token string, index map[string]remoteEntry, paths []string) (*treeChange, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var existing []byte
	if entry, ok := index[".gitattributes"]; ok {
		data, err := readGitBlob(ctx, client, owner, repo, token, entry.SHA)
		if err != nil {
			return nil, err
		}
		existing = data
	}
	content, changed := lfsAttributesContent(existing, paths)
	if !changed {
		return nil, nil
	}
	return &treeChange{Path: ".gitattributes", Mode: "100644", Source: &contentSource{inline: content}}, nil
}

func gitContentForUpload(ctx context.Context, client *http.Client, owner, repo, token, target string, source *contentSource) ([]byte, error) {
	if source == nil {
		return nil, fmt.Errorf("内部错误：缺少文件内容")
	}
	if source.inline != nil {
		return source.inline, nil
	}
	info, err := os.Stat(source.local)
	if err != nil {
		return nil, fmt.Errorf("读取本地文件失败 %s：%w", source.local, err)
	}
	if info.Size() > maxDirectGitBlobSize {
		return uploadLFSObject(ctx, client, owner, repo, token, source.local)
	}
	return readLocalFile(source.local)
}

// changePlanner builds the concrete path changes once the current repo tree is
// known. It is a callback so that moves can reuse existing blob shas without a
// second round trip.
type changePlanner func(index map[string]remoteEntry) ([]treeChange, error)

// commitChanges turns a set of path changes into exactly one commit on branch.
func (a *App) commitChanges(owner, repo, branch string, describe func(index map[string]remoteEntry) string, plan changePlanner) (*CommitResult, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	emit := func(done, total int, current string) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "task-progress", Progress{Done: done, Total: total, Current: current})
		}
	}
	client := newHTTPClient(120 * time.Second)
	token := a.effectiveToken()
	if token == "" {
		return nil, fmt.Errorf("写操作需要授权：请点右上角「认证」登录 GitHub")
	}

	emit(0, 1, "读取分支信息…")
	headSHA, treeSHA, err := branchHead(ctx, client, owner, repo, branch, token)
	if err != nil {
		return nil, err
	}
	emit(0, 1, "读取仓库文件列表…")
	index, err := treeIndex(ctx, client, owner, repo, treeSHA, token)
	if err != nil {
		return nil, err
	}

	emit(0, 1, "生成提交计划…")
	changes, err := plan(index)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("没有任何改动")
	}

	// The commit message is derived from the index so it can say "Add" vs
	// "Update" without an extra request.
	message := strings.TrimSpace(describe(index))
	if message == "" {
		message = "Update repository"
	}

	blobs := 0
	for _, ch := range changes {
		if !ch.Delete && ch.BlobSHA == "" {
			blobs++
		}
	}
	total := blobs + 3 // blobs + create tree + create commit + update ref
	done := 0
	emit(done, total, "准备文件内容…")

	entries := make([]treeEntryRequest, 0, len(changes)*2)
	for _, ch := range changes {
		target, err := repopath.CleanPath(ch.Path)
		if err != nil {
			return nil, err
		}

		if ch.Delete {
			entries = append(entries, treeEntryRequest{Path: target, Mode: "100644", Type: "blob", SHA: nil})
			continue
		}

		mode := ch.Mode
		if mode == "" {
			mode = "100644"
		}

		sha := ch.BlobSHA
		if sha == "" {
			content, err := gitContentForUpload(ctx, client, owner, repo, token, target, ch.Source)
			if err != nil {
				return nil, err
			}
			sha, err = createBlob(ctx, client, owner, repo, token, content)
			if err != nil {
				return nil, err
			}
			done++
			emit(done, total, "上传 "+target)
		}
		entries = append(entries, treeEntryRequest{Path: target, Mode: mode, Type: "blob", SHA: &sha})
	}

	// Build the new tree on top of the current one.
	emit(done, total, "创建文件树…")
	var treeOut struct {
		SHA string `json:"sha"`
	}
	treePayload := struct {
		BaseTree string             `json:"base_tree"`
		Tree     []treeEntryRequest `json:"tree"`
	}{BaseTree: treeSHA, Tree: entries}
	treeURL := fmt.Sprintf("%s/repos/%s/%s/git/trees", apiBase, owner, repo)
	if err := apiSend(ctx, client, http.MethodPost, treeURL, token, treePayload, &treeOut); err != nil {
		return nil, err
	}
	done++
	emit(done, total, "创建提交…")

	// Commit it.
	var commitOut struct {
		SHA string `json:"sha"`
	}
	commitPayload := struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
	}{Message: message, Tree: treeOut.SHA, Parents: []string{headSHA}}
	commitURL := fmt.Sprintf("%s/repos/%s/%s/git/commits", apiBase, owner, repo)
	if err := apiSend(ctx, client, http.MethodPost, commitURL, token, commitPayload, &commitOut); err != nil {
		return nil, err
	}
	done++
	emit(done, total, "更新分支…")

	// Move the branch to the new commit.
	refPayload := struct {
		SHA   string `json:"sha"`
		Force bool   `json:"force"`
	}{SHA: commitOut.SHA, Force: false}
	refURL := fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", apiBase, owner, repo, githubutil.EscapeRef(branch))
	if err := apiSend(ctx, client, http.MethodPatch, refURL, token, refPayload, nil); err != nil {
		return nil, err
	}
	done++
	emit(done, total, "完成")

	return &CommitResult{SHA: commitOut.SHA, Message: message, Changes: len(changes), Commits: 1}, nil
}

// commitStagedUpload writes a large upload through a temporary repository
// directory, then publishes all staged files to their real paths in one final
// commit. The destination paths are never partially updated.
func (a *App) commitStagedUpload(owner, repo, branch string, uploadEntries []uploadEntry, message string) (*CommitResult, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	emit := func(done, total int, current string) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "task-progress", Progress{Done: done, Total: total, Current: current})
		}
	}
	client := newHTTPClient(120 * time.Second)
	token := a.effectiveToken()
	if token == "" {
		return nil, fmt.Errorf("写操作需要授权：请点右上角「认证」登录 GitHub")
	}

	emit(0, 1, "读取分支信息…")
	headSHA, treeSHA, err := branchHead(ctx, client, owner, repo, branch, token)
	if err != nil {
		return nil, err
	}
	emit(0, 1, "读取仓库文件列表…")
	index, err := treeIndex(ctx, client, owner, repo, treeSHA, token)
	if err != nil {
		return nil, err
	}

	summary := uploadMessage(uploadEntries, index, message)
	var lfsPaths []string
	for _, e := range uploadEntries {
		if e.size > maxDirectGitBlobSize {
			lfsPaths = append(lfsPaths, e.repoPath)
		}
	}
	tempRoot := fmt.Sprintf(".repoexplorer-upload/%d", time.Now().UnixNano())
	for path := range index {
		if path == tempRoot || strings.HasPrefix(path, tempRoot+"/") {
			return nil, fmt.Errorf("临时上传目录已存在：%s", tempRoot)
		}
	}

	batchCount := (len(uploadEntries) + maxUploadFilesPerCommit - 1) / maxUploadFilesPerCommit
	total := len(uploadEntries) + batchCount*3 + 3
	done := 0
	commits := 0
	var lastSHA string

	commitTree := func(entries []treeEntryRequest, commitMessage, progress string) error {
		emit(done, total, progress+"：创建文件树…")
		var treeOut struct {
			SHA string `json:"sha"`
		}
		treePayload := struct {
			BaseTree string             `json:"base_tree"`
			Tree     []treeEntryRequest `json:"tree"`
		}{BaseTree: treeSHA, Tree: entries}
		treeURL := fmt.Sprintf("%s/repos/%s/%s/git/trees", apiBase, owner, repo)
		if err := apiSend(ctx, client, http.MethodPost, treeURL, token, treePayload, &treeOut); err != nil {
			return err
		}
		done++
		emit(done, total, progress+"：创建提交…")

		var commitOut struct {
			SHA string `json:"sha"`
		}
		commitPayload := struct {
			Message string   `json:"message"`
			Tree    string   `json:"tree"`
			Parents []string `json:"parents"`
		}{Message: commitMessage, Tree: treeOut.SHA, Parents: []string{headSHA}}
		commitURL := fmt.Sprintf("%s/repos/%s/%s/git/commits", apiBase, owner, repo)
		if err := apiSend(ctx, client, http.MethodPost, commitURL, token, commitPayload, &commitOut); err != nil {
			return err
		}
		done++
		emit(done, total, progress+"：更新分支…")

		refPayload := struct {
			SHA   string `json:"sha"`
			Force bool   `json:"force"`
		}{SHA: commitOut.SHA, Force: false}
		refURL := fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", apiBase, owner, repo, githubutil.EscapeRef(branch))
		if err := apiSend(ctx, client, http.MethodPatch, refURL, token, refPayload, nil); err != nil {
			return err
		}
		done++

		headSHA = commitOut.SHA
		treeSHA = treeOut.SHA
		lastSHA = commitOut.SHA
		commits++
		return nil
	}

	for start, batch := 0, 1; start < len(uploadEntries); start, batch = start+maxUploadFilesPerCommit, batch+1 {
		end := start + maxUploadFilesPerCommit
		if end > len(uploadEntries) {
			end = len(uploadEntries)
		}
		entries := make([]treeEntryRequest, 0, end-start)
		for _, e := range uploadEntries[start:end] {
			tempPath := repopath.Join(tempRoot, e.repoPath)
			var content []byte
			if e.size > maxDirectGitBlobSize {
				content, err = uploadLFSObject(ctx, client, owner, repo, token, e.local)
			} else {
				content, err = readLocalFile(e.local)
			}
			if err != nil {
				return nil, err
			}
			sha, err := createBlob(ctx, client, owner, repo, token, content)
			if err != nil {
				return nil, err
			}
			done++
			emit(done, total, fmt.Sprintf("第 %d/%d 批：暂存 %s", batch, batchCount, e.repoPath))
			entries = append(entries, treeEntryRequest{Path: tempPath, Mode: "100644", Type: "blob", SHA: &sha})
		}

		batchMessage := fmt.Sprintf("Stage upload files (%d/%d)", batch, batchCount)
		if err := commitTree(entries, batchMessage, fmt.Sprintf("第 %d/%d 批", batch, batchCount)); err != nil {
			return nil, err
		}
	}

	emit(done, total, "准备发布上传内容…")
	stagedIndex, err := treeIndex(ctx, client, owner, repo, treeSHA, token)
	if err != nil {
		return nil, err
	}
	publishEntries := make([]treeEntryRequest, 0, len(uploadEntries)*2+1)
	if attrChange, err := lfsAttributesChange(ctx, client, owner, repo, token, index, lfsPaths); err != nil {
		return nil, err
	} else if attrChange != nil {
		content, err := attrChange.Source.bytes()
		if err != nil {
			return nil, err
		}
		sha, err := createBlob(ctx, client, owner, repo, token, content)
		if err != nil {
			return nil, err
		}
		publishEntries = append(publishEntries, treeEntryRequest{Path: attrChange.Path, Mode: attrChange.Mode, Type: "blob", SHA: &sha})
	}
	for _, e := range uploadEntries {
		tempPath := repopath.Join(tempRoot, e.repoPath)
		staged, ok := stagedIndex[tempPath]
		if !ok {
			return nil, fmt.Errorf("临时上传文件缺失：%s", tempPath)
		}
		mode := "100644"
		if existing, ok := index[e.repoPath]; ok && existing.Mode != "" {
			mode = existing.Mode
		}
		sha := staged.SHA
		publishEntries = append(publishEntries,
			treeEntryRequest{Path: e.repoPath, Mode: mode, Type: "blob", SHA: &sha},
			treeEntryRequest{Path: tempPath, Mode: "100644", Type: "blob", SHA: nil},
		)
	}
	if err := commitTree(publishEntries, summary, "发布上传内容"); err != nil {
		return nil, err
	}

	emit(done, total, "完成")
	return &CommitResult{SHA: lastSHA, Message: summary, Changes: len(uploadEntries), Commits: commits}, nil
}

// readLocalFile reads an upload candidate from disk.
func readLocalFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取本地文件失败 %s：%w", path, err)
	}
	return data, nil
}
