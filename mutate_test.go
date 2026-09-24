package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanRepoPath(t *testing.T) {
	valid := map[string]string{
		"src/main.go":         "src/main.go",
		"/src/main.go":        "src/main.go",
		`src\main.go`:         "src/main.go",
		"  a/b/c.txt  ":       "a/b/c.txt",
		"deep/nested/file.md": "deep/nested/file.md",
	}
	for in, want := range valid {
		got, err := cleanRepoPath(in)
		if err != nil {
			t.Errorf("cleanRepoPath(%q) 报错: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("cleanRepoPath(%q) = %q，期望 %q", in, got, want)
		}
	}

	invalid := []string{"", "   ", "/", "//", "..", "../x", "a/../b", "a/./b", "a//b", "..\\x"}
	for _, in := range invalid {
		if got, err := cleanRepoPath(in); err == nil {
			t.Errorf("cleanRepoPath(%q) 应该被拒绝，却返回了 %q", in, got)
		}
	}
}

func TestCleanRepoDirAllowsRoot(t *testing.T) {
	for _, in := range []string{"", "   ", "/"} {
		got, err := cleanRepoDir(in)
		if err != nil {
			t.Errorf("cleanRepoDir(%q) 不该报错: %v", in, err)
			continue
		}
		if got != "" {
			t.Errorf("cleanRepoDir(%q) = %q，期望空字符串（仓库根目录）", in, got)
		}
	}
	if got, err := cleanRepoDir("/docs/api/"); err != nil || got != "docs/api" {
		t.Errorf("cleanRepoDir = (%q, %v)，期望 (docs/api, nil)", got, err)
	}
}

func TestJoinRepo(t *testing.T) {
	cases := []struct{ base, rel, want string }{
		{"", "a.txt", "a.txt"},
		{"docs", "a.txt", "docs/a.txt"},
		{"docs", "sub/a.txt", "docs/sub/a.txt"},
		{"docs/", "/sub/a.txt", "docs/sub/a.txt"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := joinRepo(c.base, c.rel); got != c.want {
			t.Errorf("joinRepo(%q, %q) = %q，期望 %q", c.base, c.rel, got, c.want)
		}
	}
}

func TestLocalFilePairs(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("a.txt", "aaa")
	write("sub/b.txt", "bbb")
	write(".git/config", "should be skipped")

	items, locals, err := localFilePairs("docs", []string{root})
	if err != nil {
		t.Fatalf("localFilePairs 报错: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望展开出 2 个文件（.git 被跳过），实际 %d：%+v", len(items), items)
	}
	if items[0].Path != "docs/a.txt" || items[1].Path != "docs/sub/b.txt" {
		t.Errorf("路径展开不对：%+v", items)
	}
	if len(locals) != len(items) {
		t.Errorf("locals 与 items 数量不一致：%d vs %d", len(locals), len(items))
	}
	for i := range items {
		if _, err := os.Stat(locals[i]); err != nil {
			t.Errorf("第 %d 个本地路径无效：%s", i, locals[i])
		}
	}

	// Picking a single file keeps just its base name.
	single := filepath.Join(root, "a.txt")
	items, _, err = localFilePairs("", []string{single})
	if err != nil {
		t.Fatalf("localFilePairs 报错: %v", err)
	}
	if len(items) != 1 || items[0].Path != "a.txt" {
		t.Errorf("单文件展开不对：%+v", items)
	}
}

func TestLocalFilePairsRejectsTraversal(t *testing.T) {
	if _, _, err := localFilePairs("../evil", []string{t.TempDir()}); err == nil {
		t.Error("目标目录含 .. 时应该被拒绝")
	}
}
