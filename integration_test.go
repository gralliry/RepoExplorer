package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These tests talk to the real GitHub API, so they are skipped unless you opt in:
//
//	$env:LIVE_GITHUB=1; go test -run Live -v ./...
const liveEnv = "LIVE_GITHUB"

func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv(liveEnv) == "" {
		t.Skipf("设置 %s=1 才会运行真实网络测试", liveEnv)
	}
}

func TestFetchRepoTreeLive(t *testing.T) {
	skipUnlessLive(t)

	tree, err := NewApp().FetchRepoTree("octocat/Hello-World", "")
	if err != nil {
		t.Fatalf("FetchRepoTree 失败: %v", err)
	}
	if tree.Owner != "octocat" || tree.Repo != "Hello-World" {
		t.Errorf("解析出的仓库是 %s/%s", tree.Owner, tree.Repo)
	}
	if tree.GitRef == "" || tree.DefaultBranch == "" {
		t.Errorf("git_ref / default_branch 不该为空: %q / %q", tree.GitRef, tree.DefaultBranch)
	}
	if len(tree.Branches) == 0 {
		t.Error("分支列表为空")
	}
	if len(tree.Files) == 0 {
		t.Fatal("没有解析到任何文件")
	}
	for _, f := range tree.Files {
		if f.Path == "" {
			t.Error("存在空路径的文件条目")
		}
		if f.Size <= 0 {
			t.Errorf("文件 %q 的大小不合理: %d", f.Path, f.Size)
		}
	}
	t.Logf("%s/%s @ %s：%d 个文件，分支 %v",
		tree.Owner, tree.Repo, tree.GitRef, len(tree.Files), tree.Branches)
}

func TestDownloadOneLive(t *testing.T) {
	skipUnlessLive(t)

	dest := t.TempDir()
	client := newHTTPClient(60 * time.Second)
	base := "https://raw.githubusercontent.com/octocat/Hello-World/master"

	if err := downloadOne(context.Background(), client, base, "", dest, "README"); err != nil {
		t.Fatalf("downloadOne 失败: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "README"))
	if err != nil {
		t.Fatalf("读取下载结果失败: %v", err)
	}
	if len(data) == 0 {
		t.Error("下载到的文件是空的")
	}
	t.Logf("下载成功，README 内容: %q", string(data))
}

func TestDownloadOneLiveMissingFile(t *testing.T) {
	skipUnlessLive(t)

	client := newHTTPClient(60 * time.Second)
	base := "https://raw.githubusercontent.com/octocat/Hello-World/master"
	err := downloadOne(context.Background(), client, base, "", t.TempDir(), "does-not-exist.txt")
	if err == nil {
		t.Error("下载不存在的文件应该报错")
	} else {
		t.Logf("预期内的失败: %v", err)
	}
}
