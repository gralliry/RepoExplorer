package repopath

import (
	"path/filepath"
	"testing"
)

func TestCleanPath(t *testing.T) {
	valid := map[string]string{
		"src/main.go":         "src/main.go",
		"/src/main.go":        "src/main.go",
		`src\main.go`:         "src/main.go",
		"  a/b/c.txt  ":       "a/b/c.txt",
		"deep/nested/file.md": "deep/nested/file.md",
	}
	for in, want := range valid {
		got, err := CleanPath(in)
		if err != nil {
			t.Errorf("CleanPath(%q) 报错: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("CleanPath(%q) = %q，期望 %q", in, got, want)
		}
	}

	invalid := []string{"", "   ", "/", "//", "..", "../x", "a/../b", "a/./b", "a//b", "..\\x"}
	for _, in := range invalid {
		if got, err := CleanPath(in); err == nil {
			t.Errorf("CleanPath(%q) 应该被拒绝，却返回了 %q", in, got)
		}
	}
}

func TestCleanDirAllowsRoot(t *testing.T) {
	for _, in := range []string{"", "   ", "/"} {
		got, err := CleanDir(in)
		if err != nil {
			t.Errorf("CleanDir(%q) 不该报错: %v", in, err)
			continue
		}
		if got != "" {
			t.Errorf("CleanDir(%q) = %q，期望空字符串（仓库根目录）", in, got)
		}
	}
	if got, err := CleanDir("/docs/api/"); err != nil || got != "docs/api" {
		t.Errorf("CleanDir = (%q, %v)，期望 (docs/api, nil)", got, err)
	}
}

func TestJoin(t *testing.T) {
	cases := []struct{ base, rel, want string }{
		{"", "a.txt", "a.txt"},
		{"docs", "a.txt", "docs/a.txt"},
		{"docs", "sub/a.txt", "docs/sub/a.txt"},
		{"docs/", "/sub/a.txt", "docs/sub/a.txt"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := Join(c.base, c.rel); got != c.want {
			t.Errorf("Join(%q, %q) = %q，期望 %q", c.base, c.rel, got, c.want)
		}
	}
}

func TestSafeJoin(t *testing.T) {
	if _, err := SafeJoin(`C:\dest`, "../evil.txt"); err == nil {
		t.Error("SafeJoin 应该拒绝 .. 路径")
	}
	if _, err := SafeJoin(`C:\dest`, "a/../../evil.txt"); err == nil {
		t.Error("SafeJoin 应该拒绝夹在中间的 .. 路径")
	}

	got, err := SafeJoin(`C:\dest`, "src/main.rs")
	if err != nil {
		t.Fatalf("SafeJoin 报错: %v", err)
	}
	if want := filepath.Join(`C:\dest`, "src", "main.rs"); got != want {
		t.Errorf("SafeJoin = %q，期望 %q", got, want)
	}
}
