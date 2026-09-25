package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTempFilePath(t *testing.T) {
	local, err := tempFilePath("gralliry/RepoExplorer", "main", "src/ui/main.js")
	if err != nil {
		t.Fatalf("tempFilePath: %v", err)
	}
	if !strings.Contains(local, filepath.Join("RepoExplorer", "gralliry-RepoExplorer", "main")) {
		t.Errorf("临时路径没有按 仓库/分支 分层：%s", local)
	}
	if !strings.HasSuffix(local, filepath.Join("src", "ui", "main.js")) {
		t.Errorf("没有保留目录结构：%s", local)
	}

	// Branches with slashes must not create extra nesting.
	branchy, err := tempFilePath("o/r", "release/2.x", "a.txt")
	if err != nil {
		t.Fatalf("tempFilePath: %v", err)
	}
	if !strings.Contains(branchy, "release_2.x") {
		t.Errorf("分支名里的 / 应该被替换掉：%s", branchy)
	}
}

func TestTempFilePathStaysInsideTemp(t *testing.T) {
	if _, err := tempFilePath("o/r", "main", "../escape.txt"); err == nil {
		t.Error("含 .. 的路径应该被拒绝")
	}
}

// Exercises the real download-to-temp path against GitHub. It calls the
// unexported helper on purpose: OpenFile itself would launch a program.
func TestLiveMaterializeFile(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("设置 GITHUB_TOKEN 才会运行")
	}
	t.Setenv("APPDATA", t.TempDir())

	app := New()
	if err := app.SaveManualToken(token); err != nil {
		t.Fatalf("保存 Token 失败：%v", err)
	}

	local, err := (githubProvider{}).materializeFile(app, "octocat/Hello-World", "master", "README")
	if err != nil {
		t.Fatalf("materializeFile: %v", err)
	}

	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("读取落地文件失败：%v", err)
	}
	if string(data) != "Hello World!\n" {
		t.Errorf("落地内容 = %q，期望 %q", string(data), "Hello World!\n")
	}
	t.Logf("已落地到 %s：%q", local, string(data))

	// A missing file must fail rather than silently produce an empty temp file.
	if _, err := (githubProvider{}).materializeFile(app, "octocat/Hello-World", "master", "nope.txt"); err == nil {
		t.Error("不存在的文件应该报错")
	}
}
