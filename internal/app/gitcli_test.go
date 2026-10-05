package app

import (
	"os"
	"path/filepath"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := runGit(nil, "", "--version"); err != nil {
		t.Skipf("需要本机 git 才运行通用 Git provider 测试：%v", err)
	}
}

func setupBareGitRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "seed")
	if _, err := runGit(nil, "", "init", "--bare", bare); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if _, err := runGit(nil, "", "clone", bare, work); err != nil {
		t.Fatalf("clone seed: %v", err)
	}
	if _, err := runGit(nil, work, "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("config email: %v", err)
	}
	if _, err := runGit(nil, work, "config", "user.name", "RepoExplorer Test"); err != nil {
		t.Fatalf("config name: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(nil, work, "add", "README.md"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := runGit(nil, work, "commit", "-m", "seed"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := runGit(nil, work, "branch", "-M", "main"); err != nil {
		t.Fatalf("branch: %v", err)
	}
	if _, err := runGit(nil, work, "push", "-u", "origin", "main"); err != nil {
		t.Fatalf("push: %v", err)
	}
	return bare
}

func TestGenericGitProviderFetchAndReadLocalBareRepo(t *testing.T) {
	repo := setupBareGitRepo(t)
	app := New()
	p := genericGitProvider{}

	tree, err := p.FetchRepoTree(app, repo, "")
	if err != nil {
		t.Fatalf("FetchRepoTree: %v", err)
	}
	if tree.GitRef != "main" || !tree.CanWrite {
		t.Fatalf("tree metadata 不对：%+v", tree)
	}
	if len(tree.Files) != 1 || tree.Files[0].Path != "README.md" {
		t.Fatalf("文件列表不对：%+v", tree.Files)
	}

	file, err := p.ReadFile(app, repo, "main", "README.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if file.Content != "hello\n" || file.Binary || file.TooLarge {
		t.Fatalf("文件内容不对：%+v", file)
	}
}

func TestGenericGitProviderSaveFilePushesToRemote(t *testing.T) {
	repo := setupBareGitRepo(t)
	app := New()
	p := genericGitProvider{}

	res, err := p.SaveFile(app, repo, "main", "docs/new.txt", "new content\n", "Add docs/new.txt")
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if res.Commits != 1 || res.Message != "Add docs/new.txt" {
		t.Fatalf("commit result 不对：%+v", res)
	}

	verify := filepath.Join(t.TempDir(), "verify")
	if _, err := runGit(nil, "", "clone", "--branch", "main", repo, verify); err != nil {
		t.Fatalf("clone verify: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(verify, "docs", "new.txt"))
	if err != nil {
		t.Fatalf("读取推送后的文件失败：%v", err)
	}
	if string(data) != "new content\n" {
		t.Fatalf("推送后的内容不对：%q", data)
	}
}
