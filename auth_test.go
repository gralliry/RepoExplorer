package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualTokenRoundTrip(t *testing.T) {
	// Redirect the config location into a throwaway directory.
	t.Setenv("APPDATA", t.TempDir())

	app := NewApp()
	if err := app.SaveManualToken(" ghp_abc "); err != nil {
		t.Fatalf("SaveManualToken: %v", err)
	}

	info := app.GetAuth()
	if !info.LoggedIn {
		t.Error("保存 Token 之后应该是已登录状态")
	}
	if info.Source != "manual" {
		t.Errorf("Source = %q，期望 manual", info.Source)
	}

	// A brand new App must pick the credential up from disk.
	reloaded := NewApp()
	reloaded.loadAuth()
	if got := reloaded.effectiveToken(); got != "ghp_abc" {
		t.Errorf("重新加载后 token = %q，期望去掉首尾空格的 ghp_abc", got)
	}
}

func TestClearAuth(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	app := NewApp()
	if err := app.SaveManualToken("ghp_abc"); err != nil {
		t.Fatalf("SaveManualToken: %v", err)
	}
	if err := app.ClearAuth(); err != nil {
		t.Fatalf("ClearAuth: %v", err)
	}

	if info := app.GetAuth(); info.LoggedIn {
		t.Error("ClearAuth 之后不该还是登录状态")
	}
	if got := app.effectiveToken(); got != "" {
		t.Errorf("ClearAuth 之后 token = %q，期望为空", got)
	}
}

func TestSaveManualTokenRejectsEmpty(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	if err := NewApp().SaveManualToken("   "); err == nil {
		t.Error("空 Token 应该被拒绝")
	}
}

func TestDefaultClientIDIsConfigured(t *testing.T) {
	if strings.TrimSpace(defaultClientID) == "" {
		t.Fatal("defaultClientID 不该为空，否则「用 GitHub 登录」没法用")
	}
}

func TestAuthFileLocation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)

	app := NewApp()
	if err := app.SaveManualToken("ghp_abc"); err != nil {
		t.Fatalf("SaveManualToken: %v", err)
	}

	want := filepath.Join(dir, "RepoDownloader", "auth.json")
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("期望凭据写在 %s，但读不到：%v", want, err)
	}
	if !strings.Contains(string(data), "ghp_abc") {
		t.Errorf("凭据文件内容不含 token：%s", data)
	}
}
