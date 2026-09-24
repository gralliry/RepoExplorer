package main

import (
	"os"
	"strings"
	"testing"
)

// Runs ListMyRepos — the exact code the picker calls — against the real GitHub
// API, so the affiliation grouping can be checked with real data instead of a
// mock:
//
//	$env:GITHUB_TOKEN = (gh auth token)
//	go test -run LiveListMyRepos -v ./...
func TestLiveListMyRepos(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("设置 GITHUB_TOKEN 才会运行")
	}
	t.Setenv("APPDATA", t.TempDir())

	app := NewApp()
	if err := app.SaveManualToken(token); err != nil {
		t.Fatalf("保存 Token 失败：%v", err)
	}

	repos, err := app.ListMyRepos()
	if err != nil {
		t.Fatalf("ListMyRepos: %v", err)
	}
	if len(repos) == 0 {
		t.Fatal("没有返回任何仓库")
	}

	groups := map[string][]RepoSummary{}
	for _, r := range repos {
		if r.Category == "" {
			t.Errorf("%s 没有分类", r.FullName)
		}
		groups[r.Category] = append(groups[r.Category], r)
	}

	const sample = 8
	for _, key := range []string{"owner", "organization", "collaborator"} {
		list := groups[key]
		t.Logf("=== %s（%d 个）===", key, len(list))
		for i, r := range list {
			if i >= sample {
				t.Logf("    … 还有 %d 个", len(list)-i)
				break
			}
			flag := ""
			if r.Private {
				flag = "  [私有]"
			}
			t.Logf("    %s%s", r.FullName, flag)
		}
	}

	// Self-consistency: everything in "owner" must share one owner login, and no
	// collaborator / organisation entry may use that same login.
	ownedBy := ""
	for _, r := range groups["owner"] {
		owner := strings.SplitN(r.FullName, "/", 2)[0]
		switch {
		case ownedBy == "":
			ownedBy = owner
		case !strings.EqualFold(owner, ownedBy):
			t.Errorf("owner 组里出现了两个归属：%s 和 %s", ownedBy, owner)
		}
	}
	for _, key := range []string{"collaborator", "organization"} {
		for _, r := range groups[key] {
			owner := strings.SplitN(r.FullName, "/", 2)[0]
			if strings.EqualFold(owner, ownedBy) {
				t.Errorf("%s 属于 %s，却被归到了 %s", r.FullName, ownedBy, key)
			}
		}
	}
}

// canWrite must reflect real permissions: someone else's public repository is
// download-only, one of our own is writable.
func TestLiveCanWrite(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("设置 GITHUB_TOKEN 才会运行")
	}
	t.Setenv("APPDATA", t.TempDir())

	app := NewApp()
	if err := app.SaveManualToken(token); err != nil {
		t.Fatalf("保存 Token 失败：%v", err)
	}

	other, err := app.FetchRepoTree("octocat/Hello-World", "")
	if err != nil {
		t.Fatalf("FetchRepoTree(octocat/Hello-World): %v", err)
	}
	if other.CanWrite {
		t.Error("octocat/Hello-World 是别人的公开仓库，不该判定为可写")
	}
	t.Logf("octocat/Hello-World  canWrite=%v（期望 false）", other.CanWrite)

	repos, err := app.ListMyRepos()
	if err != nil {
		t.Fatalf("ListMyRepos: %v", err)
	}
	for _, r := range repos {
		if r.Category != "owner" {
			continue
		}
		mine, err := app.FetchRepoTree(r.FullName, "")
		if err != nil {
			t.Fatalf("FetchRepoTree(%s): %v", r.FullName, err)
		}
		if !mine.CanWrite {
			t.Errorf("%s 是自己的仓库，应该判定为可写", r.FullName)
		}
		t.Logf("%s  canWrite=%v（期望 true）", r.FullName, mine.CanWrite)
		break
	}
}
