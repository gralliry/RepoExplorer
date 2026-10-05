package app

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gralliry/RepoExplorer/internal/repopath"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type genericGitProvider struct{}

func (genericGitProvider) ID() string { return "git" }

func (genericGitProvider) CanHandle(repo string) bool {
	s := strings.TrimSpace(repo)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "github.com") || strings.HasPrefix(lower, "git@github.com:") {
		return false
	}
	if strings.Contains(s, "://") || strings.HasPrefix(s, "git@") || strings.HasPrefix(s, "ssh://") {
		return true
	}
	if strings.HasSuffix(lower, ".git") || strings.Count(strings.Trim(s, "/"), "/") >= 2 {
		return true
	}
	if _, err := os.Stat(s); err == nil {
		return true
	}
	return false
}

func (a *App) tokenForGitHost(host string) string {
	host = normalizeHost(host)
	a.authMu.Lock()
	defer a.authMu.Unlock()
	a.migrateAuthLocked()
	acct := a.activeAccountLocked()
	if acct != nil && acct.Kind == "git" && normalizeHost(acct.Host) == host {
		return strings.TrimSpace(acct.Token)
	}
	return ""
}

func gitRemoteForAccount(a *App, repo string) string {
	raw := strings.TrimSpace(repo)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return raw
	}
	if token := a.tokenForGitHost(u.Host); token != "" {
		if user, pass, ok := strings.Cut(token, ":"); ok {
			u.User = url.UserPassword(user, pass)
		} else {
			u.User = url.UserPassword("oauth2", token)
		}
	}
	return u.String()
}

type gitCLIProviderContext struct {
	repo          string
	branch        string
	defaultBranch string
	branches      []string
	work          string
	cleanup       func()
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.autocrlf=false"}, args...)...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", fmt.Errorf("git %s 失败：%w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s 失败：%s", strings.Join(args, " "), text)
	}
	return text, nil
}

func gitContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func gitDefaultBranch(ctx context.Context, repo string) string {
	out, err := runGit(ctx, "", "ls-remote", "--symref", repo, "HEAD")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ref: refs/heads/") && strings.HasSuffix(line, "\tHEAD") {
				return strings.TrimSuffix(strings.TrimPrefix(line, "ref: refs/heads/"), "\tHEAD")
			}
		}
	}
	return "main"
}

func gitBranches(ctx context.Context, repo string, defaultBranch string) []string {
	out, err := runGit(ctx, "", "ls-remote", "--heads", repo)
	if err != nil {
		return []string{defaultBranch}
	}
	seen := map[string]bool{}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "refs/heads/") {
			continue
		}
		name := strings.TrimPrefix(fields[1], "refs/heads/")
		if !seen[name] {
			seen[name] = true
			branches = append(branches, name)
		}
	}
	if !seen[defaultBranch] {
		branches = append([]string{defaultBranch}, branches...)
	}
	sort.Strings(branches)
	return branches
}

func gitRepoLabel(repo string) (string, string) {
	s := strings.Trim(strings.TrimSpace(repo), "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.ReplaceAll(s, "\\", "/")
	parts := strings.Split(s, "/")
	name := "repository"
	owner := "git"
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		name = parts[len(parts)-1]
	}
	if len(parts) > 1 && parts[len(parts)-2] != "" {
		owner = strings.TrimSuffix(parts[len(parts)-2], ":")
	}
	return owner, name
}

func (genericGitProvider) checkout(a *App, repo, gitRef string, writable bool) (*gitCLIProviderContext, error) {
	ctx := gitContext(a.ctx)
	remote := gitRemoteForAccount(a, repo)
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("未找到 git 命令，请先安装 Git 并加入 PATH")
	}
	defaultBranch := gitDefaultBranch(ctx, remote)
	branches := gitBranches(ctx, remote, defaultBranch)
	branch := strings.TrimSpace(gitRef)
	if branch == "" {
		branch = defaultBranch
	}

	base, err := os.MkdirTemp("", "RepoExplorer-git-")
	if err != nil {
		return nil, fmt.Errorf("创建临时工作区失败：%w", err)
	}
	cleanup := func() { _ = os.RemoveAll(base) }
	work := filepath.Join(base, "work")
	args := []string{"clone", "--branch", branch}
	if !writable {
		args = append(args, "--depth", "1")
	}
	args = append(args, remote, work)
	if _, err := runGit(ctx, "", args...); err != nil {
		cleanup()
		return nil, err
	}
	return &gitCLIProviderContext{repo: repo, branch: branch, defaultBranch: defaultBranch, branches: branches, work: work, cleanup: cleanup}, nil
}

func gitWalkFiles(root string) ([]FileEntry, error) {
	var files []FileEntry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, FileEntry{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (p genericGitProvider) FetchRepoTree(a *App, repo, gitRef string) (*RepoTree, error) {
	co, err := p.checkout(a, repo, gitRef, false)
	if err != nil {
		return nil, err
	}
	defer co.cleanup()
	files, err := gitWalkFiles(co.work)
	if err != nil {
		return nil, err
	}
	owner, name := gitRepoLabel(repo)
	return &RepoTree{Owner: owner, Repo: name, Provider: "git", GitRef: co.branch, DefaultBranch: co.defaultBranch, Branches: co.branches, Files: files, CanWrite: true}, nil
}

func copyFilePreserve(destRoot, rel, srcRoot string) error {
	target, err := repopath.SafeJoin(destRoot, rel)
	if err != nil {
		return err
	}
	src, err := repopath.SafeJoin(srcRoot, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (p genericGitProvider) DownloadFiles(a *App, repo, gitRef, dest string, paths []string) (*DownloadResult, error) {
	if strings.TrimSpace(dest) == "" {
		return nil, fmt.Errorf("未选择下载目录")
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("没有需要下载的文件")
	}
	co, err := p.checkout(a, repo, gitRef, false)
	if err != nil {
		return nil, err
	}
	defer co.cleanup()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, fmt.Errorf("无法创建下载目录：%w", err)
	}
	var problems []string
	for i, path := range paths {
		if err := copyFilePreserve(dest, path, co.work); err != nil {
			problems = append(problems, fmt.Sprintf("%s → %v", path, err))
		}
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "download-progress", Progress{Done: i + 1, Total: len(paths), Current: path, Failed: len(problems)})
		}
	}
	return &DownloadResult{Downloaded: len(paths) - len(problems), Failed: len(problems), Errors: problems}, nil
}

func (p genericGitProvider) OpenFile(a *App, repo, branch, repoPath string) (string, error) {
	co, err := p.checkout(a, repo, branch, false)
	if err != nil {
		return "", err
	}
	defer co.cleanup()
	target, err := repopath.CleanPath(repoPath)
	if err != nil {
		return "", err
	}
	src, err := repopath.SafeJoin(co.work, target)
	if err != nil {
		return "", err
	}
	owner, name := gitRepoLabel(repo)
	dest, err := tempFilePath(owner+"/"+name, co.branch, target)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("创建临时目录失败：%w", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("读取文件失败：%w", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", fmt.Errorf("写入临时文件失败：%w", err)
	}
	if err := openWithSystemDefault(dest); err != nil {
		return dest, err
	}
	return dest, nil
}

func (p genericGitProvider) ReadFile(a *App, repo, branch, filePath string) (*FileContent, error) {
	co, err := p.checkout(a, repo, branch, false)
	if err != nil {
		return nil, err
	}
	defer co.cleanup()
	target, err := repopath.CleanPath(filePath)
	if err != nil {
		return nil, err
	}
	path, err := repopath.SafeJoin(co.work, target)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("文件不存在：%s", target)
	}
	result := &FileContent{Path: target, Size: info.Size()}
	if info.Size() > maxEditableSize {
		result.TooLarge = true
		return result, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		result.Binary = true
		return result, nil
	}
	result.Content = string(data)
	return result, nil
}

func (p genericGitProvider) commitAndPush(a *App, repo, branch, message string, mutate func(work string) error) (*CommitResult, error) {
	co, err := p.checkout(a, repo, branch, true)
	if err != nil {
		return nil, err
	}
	defer co.cleanup()
	if err := mutate(co.work); err != nil {
		return nil, err
	}
	ctx := gitContext(a.ctx)
	if _, err := runGit(ctx, co.work, "add", "-A"); err != nil {
		return nil, err
	}
	status, err := runGit(ctx, co.work, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return nil, fmt.Errorf("没有任何改动")
	}
	changes := len(strings.Split(status, "\n"))
	if strings.TrimSpace(message) == "" {
		message = "Update repository"
	}
	if _, err := runGit(ctx, co.work, "commit", "-m", message); err != nil {
		return nil, err
	}
	sha, err := runGit(ctx, co.work, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if _, err := runGit(ctx, co.work, "push", "origin", "HEAD:"+co.branch); err != nil {
		return nil, err
	}
	return &CommitResult{SHA: strings.TrimSpace(sha), Message: message, Changes: changes, Commits: 1}, nil
}

func (p genericGitProvider) SaveFile(a *App, repo, branch, filePath, content, message string) (*CommitResult, error) {
	target, err := repopath.CleanPath(filePath)
	if err != nil {
		return nil, err
	}
	return p.commitAndPush(a, repo, branch, defaultMessage(message, "Update %s", target), func(work string) error {
		path, err := repopath.SafeJoin(work, target)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(content), 0o644)
	})
}

func (p genericGitProvider) DeletePaths(a *App, repo, branch string, paths []string, message string) (*CommitResult, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("没有选中任何要删除的文件")
	}
	return p.commitAndPush(a, repo, branch, defaultMessage(message, "Delete %d files", len(paths)), func(work string) error {
		for _, path := range paths {
			target, err := repopath.SafeJoin(work, path)
			if err != nil {
				return err
			}
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}

func (p genericGitProvider) MovePaths(a *App, repo, branch string, moves []PathMove, message string) (*CommitResult, error) {
	if len(moves) == 0 {
		return nil, fmt.Errorf("没有需要移动的文件")
	}
	return p.commitAndPush(a, repo, branch, defaultMessage(message, "Move %d files", len(moves)), func(work string) error {
		for _, move := range moves {
			from, err := repopath.SafeJoin(work, move.From)
			if err != nil {
				return err
			}
			to, err := repopath.SafeJoin(work, move.To)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			if err := os.Rename(from, to); err != nil {
				return err
			}
		}
		return nil
	})
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("只能复制文件")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (p genericGitProvider) CopyPaths(a *App, repo, branch string, copies []PathMove, message string) (*CommitResult, error) {
	if len(copies) == 0 {
		return nil, fmt.Errorf("没有需要复制的文件")
	}
	return p.commitAndPush(a, repo, branch, defaultMessage(message, "Copy %d files", len(copies)), func(work string) error {
		for _, copy := range copies {
			from, err := repopath.SafeJoin(work, copy.From)
			if err != nil {
				return err
			}
			to, err := repopath.SafeJoin(work, copy.To)
			if err != nil {
				return err
			}
			if err := copyPath(from, to); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p genericGitProvider) UploadFiles(a *App, repo, branch, repoDir string, localPaths []string, message string) (*CommitResult, error) {
	items, locals, err := localFilePairs(repoDir, localPaths)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("没有可上传的文件")
	}
	return p.commitAndPush(a, repo, branch, defaultMessage(message, "Upload %d files", len(items)), func(work string) error {
		for i, item := range items {
			dest, err := repopath.SafeJoin(work, item.Path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			data, err := os.ReadFile(locals[i])
			if err != nil {
				return err
			}
			if err := os.WriteFile(dest, data, 0o644); err != nil {
				return err
			}
		}
		return nil
	})
}

var _ gitProvider = genericGitProvider{}
