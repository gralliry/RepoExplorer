package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The write side is exercised against a real throwaway repository, because
// there is no sane way to fake the Git Data API dance.
//
//	$env:GITHUB_TOKEN     = (gh auth token)
//	$env:GITHUB_TEST_REPO = "you/RepoExplorer-scratch"
//	go test -run Live -v ./...

func liveWriteEnv(t *testing.T) (*App, string, string) {
	t.Helper()
	token := os.Getenv("GITHUB_TOKEN")
	repo := os.Getenv("GITHUB_TEST_REPO")
	if token == "" || repo == "" {
		t.Skip("设置 GITHUB_TOKEN 和 GITHUB_TEST_REPO 才会运行写操作测试")
	}
	t.Setenv("APPDATA", t.TempDir())

	app := NewApp()
	if err := app.SaveManualToken(token); err != nil {
		t.Fatalf("保存测试用 Token 失败：%v", err)
	}
	return app, repo, token
}

func liveTreePaths(t *testing.T, app *App, repo, branch, prefix string) []string {
	t.Helper()
	tree, err := app.FetchRepoTree(repo, branch)
	if err != nil {
		t.Fatalf("FetchRepoTree: %v", err)
	}
	var paths []string
	for _, f := range tree.Files {
		if strings.HasPrefix(f.Path, prefix) {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

// commitParents returns the parent shas of a commit, proving how many commits an
// operation produced.
func commitParents(t *testing.T, repo, sha, token string) []string {
	t.Helper()
	owner, name, err := parseRepo(repo)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	url := apiBase + "/repos/" + owner + "/" + name + "/git/commits/" + sha
	if err := apiGet(context.Background(), newHTTPClient(30*time.Second), url, token, &out); err != nil {
		t.Fatalf("读取 commit 失败：%v", err)
	}
	parents := make([]string, 0, len(out.Parents))
	for _, p := range out.Parents {
		parents = append(parents, p.SHA)
	}
	return parents
}

func TestLiveWriteLifecycle(t *testing.T) {
	app, repo, token := liveWriteEnv(t)
	const branch = "main"
	const prefix = "livetest/"

	purge := func() {
		if paths := liveTreePaths(t, app, repo, branch, prefix); len(paths) > 0 {
			if _, err := app.DeletePaths(repo, branch, paths, "chore: clean scratch"); err != nil {
				t.Fatalf("清理测试目录失败：%v", err)
			}
		}
	}
	purge()
	defer purge()

	// ---- 1. create ------------------------------------------------------
	res, err := app.SaveFile(repo, branch, prefix+"hello.txt", "v1\n", "")
	if err != nil {
		t.Fatalf("SaveFile(create): %v", err)
	}
	if res.Message != "Add "+prefix+"hello.txt" {
		t.Errorf("新建时的 commit message = %q", res.Message)
	}

	file, err := app.ReadFile(repo, branch, prefix+"hello.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if file.Content != "v1\n" || file.Binary || file.TooLarge {
		t.Errorf("读回的内容不对：%+v", file)
	}

	// ---- 2. update ------------------------------------------------------
	res, err = app.SaveFile(repo, branch, prefix+"hello.txt", "v2\n", "")
	if err != nil {
		t.Fatalf("SaveFile(update): %v", err)
	}
	if res.Message != "Update "+prefix+"hello.txt" {
		t.Errorf("更新时的 commit message = %q", res.Message)
	}
	if file, _ = app.ReadFile(repo, branch, prefix+"hello.txt"); file.Content != "v2\n" {
		t.Errorf("更新后内容 = %q", file.Content)
	}

	// ---- 3. rename in place --------------------------------------------
	res, err = app.MovePaths(repo, branch, []PathMove{
		{From: prefix + "hello.txt", To: prefix + "renamed.txt"},
	}, "")
	if err != nil {
		t.Fatalf("MovePaths: %v", err)
	}
	if res.Message != "Rename hello.txt to renamed.txt" {
		t.Errorf("重命名的 commit message = %q", res.Message)
	}
	if file, err = app.ReadFile(repo, branch, prefix+"renamed.txt"); err != nil {
		t.Fatalf("重命名后读取失败：%v", err)
	}
	if file.Content != "v2\n" {
		t.Errorf("重命名后内容 = %q", file.Content)
	}
	if _, err := app.ReadFile(repo, branch, prefix+"hello.txt"); err == nil {
		t.Error("旧路径应该已经不存在了")
	}

	// ---- 4. upload a local folder --------------------------------------
	local := t.TempDir()
	if err := os.MkdirAll(filepath.Join(local, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(local, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(local, "one.txt"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(local, "sub", "two.txt"), []byte("2"), 0o644)
	os.WriteFile(filepath.Join(local, ".git", "config"), []byte("skip me"), 0o644)

	items, err := app.PlanUpload(prefix+"uploaded", []string{local})
	if err != nil {
		t.Fatalf("PlanUpload: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("应该展开出 2 个文件（跳过 .git），实际 %+v", items)
	}

	res, err = app.UploadFiles(repo, branch, prefix+"uploaded", []string{local}, "")
	if err != nil {
		t.Fatalf("UploadFiles: %v", err)
	}
	if res.Message != "Add 2 files" {
		t.Errorf("上传的 commit message = %q", res.Message)
	}
	if file, err = app.ReadFile(repo, branch, prefix+"uploaded/sub/two.txt"); err != nil {
		t.Fatalf("上传后读取失败：%v", err)
	}
	if file.Content != "2" {
		t.Errorf("上传内容 = %q", file.Content)
	}

	// ---- 5. move a whole folder, and prove it is ONE commit -------------
	owner, name, _ := parseRepo(repo)
	client := newHTTPClient(30 * time.Second)
	headBefore, _, err := branchHead(context.Background(), client, owner, name, branch, token)
	if err != nil {
		t.Fatalf("读取分支头失败：%v", err)
	}

	var moves []PathMove
	for _, p := range liveTreePaths(t, app, repo, branch, prefix+"uploaded/") {
		moves = append(moves, PathMove{
			From: p,
			To:   prefix + "moved/" + strings.TrimPrefix(p, prefix+"uploaded/"),
		})
	}
	if len(moves) != 2 {
		t.Fatalf("应该有 2 个文件需要移动：%+v", moves)
	}
	res, err = app.MovePaths(repo, branch, moves, "")
	if err != nil {
		t.Fatalf("移动文件夹失败：%v", err)
	}
	if res.Message != "Move 2 files" {
		t.Errorf("移动文件夹的 commit message = %q", res.Message)
	}

	parents := commitParents(t, repo, res.SHA, token)
	if len(parents) != 1 || parents[0] != headBefore {
		t.Errorf("移动文件夹应该只产生 1 个提交（父提交=%v，移动前分支头=%s）", parents, headBefore)
	}
	if file, err = app.ReadFile(repo, branch, prefix+"moved/sub/two.txt"); err != nil {
		t.Errorf("移动后读取失败：%v", err)
	}
	if _, err := app.ReadFile(repo, branch, prefix+"uploaded/sub/two.txt"); err == nil {
		t.Error("移动后旧路径应该不存在了")
	}

	// ---- 6. delete ------------------------------------------------------
	all := liveTreePaths(t, app, repo, branch, prefix)
	if len(all) != 3 { // renamed.txt + moved/one.txt + moved/sub/two.txt
		t.Fatalf("删除前应该有 3 个文件，实际 %v", all)
	}
	res, err = app.DeletePaths(repo, branch, all, "")
	if err != nil {
		t.Fatalf("DeletePaths: %v", err)
	}
	if res.Message != "Delete 3 files" {
		t.Errorf("删除的 commit message = %q", res.Message)
	}
	if left := liveTreePaths(t, app, repo, branch, prefix); len(left) != 0 {
		t.Errorf("删除后仍有残留：%v", left)
	}
}

func TestLiveWriteRejectsConflict(t *testing.T) {
	app, repo, _ := liveWriteEnv(t)
	const branch = "main"
	const prefix = "livetest/"

	purge := func() {
		if paths := liveTreePaths(t, app, repo, branch, prefix); len(paths) > 0 {
			app.DeletePaths(repo, branch, paths, "chore: clean scratch")
		}
	}
	purge()
	defer purge()

	if _, err := app.SaveFile(repo, branch, prefix+"a.txt", "a", ""); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if _, err := app.SaveFile(repo, branch, prefix+"b.txt", "b", ""); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}

	// Moving onto an existing path must be refused rather than silently
	// overwriting somebody's file.
	if _, err := app.MovePaths(repo, branch, []PathMove{
		{From: prefix + "a.txt", To: prefix + "b.txt"},
	}, ""); err == nil {
		t.Error("移动到已存在的路径应该被拒绝")
	}

	// Paths that try to escape the repo must be refused too.
	if _, err := app.SaveFile(repo, branch, "../evil.txt", "x", ""); err == nil {
		t.Error("含 .. 的路径应该被拒绝")
	}
}

func TestLiveWriteRequiresAuth(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	app := NewApp()

	_, err := app.SaveFile("octocat/Hello-World", "master", "x.txt", "x", "")
	if err == nil {
		t.Fatal("没有登录时写操作应该报错")
	}
	if !strings.Contains(err.Error(), "认证") {
		t.Errorf("错误信息应该提示需要登录，实际：%v", err)
	}
}
