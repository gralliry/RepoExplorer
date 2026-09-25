package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gralliry/RepoExplorer/internal/githubutil"
	"github.com/gralliry/RepoExplorer/internal/repopath"
)

// OpenFile downloads a file from the repository into a local temp folder and
// hands it to the operating system, so it opens in whatever application the
// user has associated with that file type — the same thing double-clicking a
// file does in Explorer.
//
// The app deliberately does not preview or edit file contents itself; it only
// materialises the file and lets the OS take over. The local copy is
// throwaway: edits made there are not written back to the repository (use
// Upload for that).
func (p githubProvider) OpenFile(a *App, repo, branch, repoPath string) (string, error) {
	dest, err := p.materializeFile(a, repo, branch, repoPath)
	if err != nil {
		return "", err
	}
	if err := openWithSystemDefault(dest); err != nil {
		return dest, err
	}
	return dest, nil
}

// materializeFile downloads a repo file into the temp folder and returns the
// local path. Kept unexported (and separate from OpenFile) so tests can verify
// the download without launching a program on the developer's desktop.
func (githubProvider) materializeFile(a *App, repo, branch, repoPath string) (string, error) {
	owner, name, err := githubutil.ParseRepo(repo)
	if err != nil {
		return "", err
	}
	target, err := repopath.CleanPath(repoPath)
	if err != nil {
		return "", err
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	client := newHTTPClient(300 * time.Second)
	token := a.effectiveToken()

	_, treeSHA, err := branchHead(ctx, client, owner, name, branch, token)
	if err != nil {
		return "", err
	}
	index, err := treeIndex(ctx, client, owner, name, treeSHA, token)
	if err != nil {
		return "", err
	}
	entry, ok := index[target]
	if !ok {
		return "", fmt.Errorf("文件不存在：%s", target)
	}

	// The blobs endpoint is binary-safe, unlike the contents endpoint which
	// refuses anything it cannot inline as base64 text.
	var blob struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/blobs/%s", apiBase, owner, name, entry.SHA)
	if err := apiGet(ctx, client, url, token, &blob); err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("解码文件内容失败：%w", err)
	}

	dest, err := tempFilePath(owner+"/"+name, branch, target)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("创建临时目录失败：%w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", fmt.Errorf("写入临时文件失败：%w", err)
	}

	return dest, nil
}

// tempFilePath maps a repo-relative path onto a stable temp location, keeping
// the directory structure so that files referencing their neighbours still work.
func tempFilePath(repo, ref, repoPath string) (string, error) {
	safeRepo := strings.NewReplacer("/", "-", "\\", "-").Replace(repo)
	safeRef := strings.NewReplacer("/", "_", "\\", "_").Replace(ref)

	base := filepath.Join(os.TempDir(), "RepoExplorer", safeRepo, safeRef)
	full := filepath.Join(base, filepath.FromSlash(repoPath))

	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("非法路径：%s", repoPath)
	}
	return full, nil
}

// TempFolder is where opened files are materialised, shown in the settings.
func (a *App) TempFolder() string {
	return filepath.Join(os.TempDir(), "RepoExplorer")
}

// OpenTempFolder opens that folder in the file manager.
func (a *App) OpenTempFolder() error {
	dir := a.TempFolder()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建临时目录失败：%w", err)
	}
	return openWithSystemDefault(dir)
}
