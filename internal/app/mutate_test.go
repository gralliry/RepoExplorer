package app

import (
	"os"
	"path/filepath"
	"testing"
)

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
	folderName := filepath.Base(root)
	if items[0].Path != filepath.ToSlash(filepath.Join("docs", folderName, "a.txt")) ||
		items[1].Path != filepath.ToSlash(filepath.Join("docs", folderName, "sub", "b.txt")) {
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
