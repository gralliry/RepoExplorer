package main

import (
	"path/filepath"
	"testing"
)

func TestParseRepo(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		repo  string
	}{
		{"tauri-apps/tauri", "tauri-apps", "tauri"},
		{"  tauri-apps/tauri  ", "tauri-apps", "tauri"},
		{"https://github.com/tauri-apps/tauri", "tauri-apps", "tauri"},
		{"https://github.com/tauri-apps/tauri.git/", "tauri-apps", "tauri"},
		{"http://www.github.com/tauri-apps/tauri", "tauri-apps", "tauri"},
		{"git@github.com:tauri-apps/tauri.git", "tauri-apps", "tauri"},
		{"https://github.com/tauri-apps/tauri/tree/dev/src", "tauri-apps", "tauri"},
	}

	for _, c := range cases {
		owner, repo, err := parseRepo(c.in)
		if err != nil {
			t.Errorf("parseRepo(%q) 报错: %v", c.in, err)
			continue
		}
		if owner != c.owner || repo != c.repo {
			t.Errorf("parseRepo(%q) = (%q, %q)，期望 (%q, %q)",
				c.in, owner, repo, c.owner, c.repo)
		}
	}
}

func TestParseRepoRejectsInvalid(t *testing.T) {
	for _, in := range []string{"just-a-name", "   ", ""} {
		if _, _, err := parseRepo(in); err == nil {
			t.Errorf("parseRepo(%q) 应该报错", in)
		}
	}
}

func TestEscapeRefKeepsSlashes(t *testing.T) {
	cases := map[string]string{
		"main":        "main",
		"release/2.x": "release/2.x",
		"feature/a b": "feature/a%20b",
		"docs/说明.md":  "docs/%E8%AF%B4%E6%98%8E.md",
	}
	for in, want := range cases {
		if got := escapeRef(in); got != want {
			t.Errorf("escapeRef(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestSafeJoin(t *testing.T) {
	if _, err := safeJoin(`C:\dest`, "../evil.txt"); err == nil {
		t.Error("safeJoin 应该拒绝 .. 路径")
	}
	if _, err := safeJoin(`C:\dest`, "a/../../evil.txt"); err == nil {
		t.Error("safeJoin 应该拒绝夹在中间的 .. 路径")
	}

	got, err := safeJoin(`C:\dest`, "src/main.rs")
	if err != nil {
		t.Fatalf("safeJoin 报错: %v", err)
	}
	if want := filepath.Join(`C:\dest`, "src", "main.rs"); got != want {
		t.Errorf("safeJoin = %q，期望 %q", got, want)
	}
}
