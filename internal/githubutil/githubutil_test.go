package githubutil

import "testing"

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
		owner, repo, err := ParseRepo(c.in)
		if err != nil {
			t.Errorf("ParseRepo(%q) 报错: %v", c.in, err)
			continue
		}
		if owner != c.owner || repo != c.repo {
			t.Errorf("ParseRepo(%q) = (%q, %q)，期望 (%q, %q)", c.in, owner, repo, c.owner, c.repo)
		}
	}
}

func TestParseRepoRejectsInvalid(t *testing.T) {
	for _, in := range []string{"just-a-name", "   ", ""} {
		if _, _, err := ParseRepo(in); err == nil {
			t.Errorf("ParseRepo(%q) 应该报错", in)
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
		if got := EscapeRef(in); got != want {
			t.Errorf("EscapeRef(%q) = %q，期望 %q", in, got, want)
		}
	}
}
