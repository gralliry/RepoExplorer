package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// maxEditableSize caps what the built-in editor will open.
const maxEditableSize = 2 << 20 // 2 MiB

// PathMove is one source -> destination pair for rename / move.
type PathMove struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// UploadItem is one file an upload is going to write.
type UploadItem struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// FileContent is what the editor receives.
type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"tooLarge"`
}

/* ------------------------------------------------------------ path helpers */

// cleanRepoDir is like cleanRepoPath but allows "" (the repository root).
func cleanRepoDir(dir string) (string, error) {
	dir = strings.Trim(strings.ReplaceAll(strings.TrimSpace(dir), "\\", "/"), "/")
	if dir == "" {
		return "", nil
	}
	return cleanRepoPath(dir)
}

func joinRepo(base, rel string) string {
	base = strings.Trim(strings.ReplaceAll(base, "\\", "/"), "/")
	rel = strings.Trim(strings.ReplaceAll(rel, "\\", "/"), "/")
	switch {
	case base == "":
		return rel
	case rel == "":
		return base
	default:
		return base + "/" + rel
	}
}

// localFilePairs expands the picked local paths (files or folders) into
// local -> repo path pairs, skipping .git directories.
func localFilePairs(repoDir string, localPaths []string) ([]UploadItem, []string, error) {
	base, err := cleanRepoDir(repoDir)
	if err != nil {
		return nil, nil, err
	}

	items := make([]UploadItem, 0, len(localPaths))
	locals := make([]string, 0, len(localPaths))

	addFile := func(local, rel string) error {
		info, err := os.Stat(local)
		if err != nil {
			return fmt.Errorf("读取本地文件失败 %s：%w", local, err)
		}
		items = append(items, UploadItem{Path: joinRepo(base, rel), Size: info.Size()})
		locals = append(locals, local)
		return nil
	}

	for _, local := range localPaths {
		info, err := os.Stat(local)
		if err != nil {
			return nil, nil, fmt.Errorf("读取本地路径失败 %s：%w", local, err)
		}

		if !info.IsDir() {
			if err := addFile(local, info.Name()); err != nil {
				return nil, nil, err
			}
			continue
		}

		root := local
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return fs.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			return addFile(p, filepath.ToSlash(rel))
		})
		if err != nil {
			return nil, nil, err
		}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, locals, nil
}

/* ------------------------------------------------------------ local pickers */

// PickUploadFiles opens the native picker for one or more local files.
func (a *App) PickUploadFiles() []string {
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要上传的文件",
	})
	if err != nil {
		return nil
	}
	return files
}

// PickUploadFolder opens the native picker for a local folder.
func (a *App) PickUploadFolder() string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择要上传的文件夹",
	})
	if err != nil {
		return ""
	}
	return dir
}

/* --------------------------------------------------------------- read file */

// ReadFile returns the content of a text file so the editor can show it.
func (a *App) ReadFile(repo, branch, filePath string) (*FileContent, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}
	target, err := cleanRepoPath(filePath)
	if err != nil {
		return nil, err
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	client := newHTTPClient(60 * time.Second)
	token := a.effectiveToken()

	_, treeSHA, err := branchHead(ctx, client, owner, name, branch, token)
	if err != nil {
		return nil, err
	}
	index, err := treeIndex(ctx, client, owner, name, treeSHA, token)
	if err != nil {
		return nil, err
	}
	entry, ok := index[target]
	if !ok {
		return nil, fmt.Errorf("文件不存在：%s", target)
	}

	result := &FileContent{Path: target, Size: entry.Size}
	if entry.Size > maxEditableSize {
		result.TooLarge = true
		return result, nil
	}

	var blob struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	url := fmt.Sprintf("%s/repos/%s/%s/git/blobs/%s", apiBase, owner, name, entry.SHA)
	if err := apiGet(ctx, client, url, token, &blob); err != nil {
		return nil, err
	}

	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("解码文件内容失败：%w", err)
	}
	if !utf8.Valid(data) {
		result.Binary = true
		return result, nil
	}

	result.Content = string(data)
	return result, nil
}

/* ------------------------------------------------------------------ writes */

// SaveFile creates or overwrites a text file (used by the editor).
func (a *App) SaveFile(repo, branch, filePath, content, message string) (*CommitResult, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}
	target, err := cleanRepoPath(filePath)
	if err != nil {
		return nil, err
	}

	plan := func(index map[string]remoteEntry) ([]treeChange, error) {
		mode := "100644"
		if existing, ok := index[target]; ok && existing.Mode != "" {
			mode = existing.Mode
		}
		return []treeChange{{
			Path:   target,
			Mode:   mode,
			Source: &contentSource{inline: []byte(content)},
		}}, nil
	}

	describe := func(index map[string]remoteEntry) string {
		if strings.TrimSpace(message) != "" {
			return message
		}
		if _, exists := index[target]; exists {
			return "Update " + target
		}
		return "Add " + target
	}

	return a.commitChanges(owner, name, branch, describe, plan)
}

// DeletePaths removes the given repo paths in a single commit.
func (a *App) DeletePaths(repo, branch string, paths []string, message string) (*CommitResult, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	targets := make([]string, 0, len(paths))
	for _, p := range paths {
		cleaned, err := cleanRepoPath(p)
		if err != nil {
			return nil, err
		}
		targets = append(targets, cleaned)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("没有选中任何要删除的文件")
	}

	plan := func(index map[string]remoteEntry) ([]treeChange, error) {
		changes := make([]treeChange, 0, len(targets))
		for _, t := range targets {
			if _, ok := index[t]; !ok {
				return nil, fmt.Errorf("文件已不存在（可能被其他人改动过）：%s", t)
			}
			changes = append(changes, treeChange{Path: t, Delete: true})
		}
		return changes, nil
	}

	describe := func(map[string]remoteEntry) string {
		if strings.TrimSpace(message) != "" {
			return message
		}
		if len(targets) == 1 {
			return "Delete " + targets[0]
		}
		return fmt.Sprintf("Delete %d files", len(targets))
	}

	return a.commitChanges(owner, name, branch, describe, plan)
}

// MovePaths renames or moves files in a single commit. Folder moves are expanded
// by the caller into one entry per file.
func (a *App) MovePaths(repo, branch string, moves []PathMove, message string) (*CommitResult, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	type pair struct{ from, to string }
	list := make([]pair, 0, len(moves))
	seenTargets := make(map[string]bool, len(moves))

	for _, m := range moves {
		from, err := cleanRepoPath(m.From)
		if err != nil {
			return nil, err
		}
		to, err := cleanRepoPath(m.To)
		if err != nil {
			return nil, err
		}
		if from == to {
			continue
		}
		if seenTargets[to] {
			return nil, fmt.Errorf("有多个文件的目标路径重复：%s", to)
		}
		seenTargets[to] = true
		list = append(list, pair{from: from, to: to})
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("没有需要移动的文件")
	}

	plan := func(index map[string]remoteEntry) ([]treeChange, error) {
		changes := make([]treeChange, 0, len(list)*2)
		for _, p := range list {
			src, ok := index[p.from]
			if !ok {
				return nil, fmt.Errorf("源文件不存在：%s", p.from)
			}
			if _, exists := index[p.to]; exists {
				return nil, fmt.Errorf("目标已存在：%s", p.to)
			}
			mode := src.Mode
			if mode == "" {
				mode = "100644"
			}
			changes = append(changes,
				treeChange{Path: p.to, Mode: mode, BlobSHA: src.SHA},
				treeChange{Path: p.from, Delete: true},
			)
		}
		return changes, nil
	}

	describe := func(map[string]remoteEntry) string {
		if strings.TrimSpace(message) != "" {
			return message
		}
		if len(list) == 1 {
			base := func(p string) string {
				if i := strings.LastIndex(p, "/"); i >= 0 {
					return p[i+1:]
				}
				return p
			}
			if dirOf(list[0].from) == dirOf(list[0].to) {
				return fmt.Sprintf("Rename %s to %s", base(list[0].from), base(list[0].to))
			}
			return fmt.Sprintf("Move %s to %s", list[0].from, list[0].to)
		}
		return fmt.Sprintf("Move %d files", len(list))
	}

	return a.commitChanges(owner, name, branch, describe, plan)
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// CopyPaths duplicates files in a single commit. Contents are not re-uploaded:
// the new paths simply reuse the existing blob shas, so copying a 10 MB file
// costs the same as copying a 10 byte one. Folder copies are expanded by the
// caller into one entry per file.
func (a *App) CopyPaths(repo, branch string, copies []PathMove, message string) (*CommitResult, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	type pair struct{ from, to string }
	list := make([]pair, 0, len(copies))
	seenTargets := make(map[string]bool, len(copies))

	for _, c := range copies {
		from, err := cleanRepoPath(c.From)
		if err != nil {
			return nil, err
		}
		to, err := cleanRepoPath(c.To)
		if err != nil {
			return nil, err
		}
		if from == to {
			continue
		}
		if seenTargets[to] {
			return nil, fmt.Errorf("有多个文件的目标路径重复：%s", to)
		}
		seenTargets[to] = true
		list = append(list, pair{from: from, to: to})
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("没有需要复制的文件")
	}

	plan := func(index map[string]remoteEntry) ([]treeChange, error) {
		changes := make([]treeChange, 0, len(list))
		for _, p := range list {
			src, ok := index[p.from]
			if !ok {
				return nil, fmt.Errorf("源文件不存在：%s", p.from)
			}
			if _, exists := index[p.to]; exists {
				return nil, fmt.Errorf("目标已存在：%s", p.to)
			}
			mode := src.Mode
			if mode == "" {
				mode = "100644"
			}
			// Reuse the blob: no content is transferred.
			changes = append(changes, treeChange{Path: p.to, Mode: mode, BlobSHA: src.SHA})
		}
		return changes, nil
	}

	describe := func(map[string]remoteEntry) string {
		if strings.TrimSpace(message) != "" {
			return message
		}
		if len(list) == 1 {
			return fmt.Sprintf("Copy %s to %s", list[0].from, list[0].to)
		}
		return fmt.Sprintf("Copy %d files", len(list))
	}

	return a.commitChanges(owner, name, branch, describe, plan)
}

// PlanUpload expands picked local paths into the repo paths they will become,
// so the UI can show a confirmation list before anything is written.
func (a *App) PlanUpload(repoDir string, localPaths []string) ([]UploadItem, error) {
	if len(localPaths) == 0 {
		return nil, fmt.Errorf("没有选择任何文件")
	}
	items, _, err := localFilePairs(repoDir, localPaths)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("选中的文件夹里没有文件")
	}
	return items, nil
}

// UploadFiles writes local files (or whole folders) into the repository as one
// atomic commit.
func (a *App) UploadFiles(repo, branch, repoDir string, localPaths []string, message string) (*CommitResult, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	items, locals, err := localFilePairs(repoDir, localPaths)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("没有可上传的文件")
	}

	type entry struct {
		repoPath string
		local    string
	}
	entries := make([]entry, len(items))
	for i := range items {
		entries[i] = entry{repoPath: items[i].Path, local: locals[i]}
	}

	plan := func(index map[string]remoteEntry) ([]treeChange, error) {
		changes := make([]treeChange, 0, len(entries))
		for _, e := range entries {
			mode := "100644"
			if existing, ok := index[e.repoPath]; ok && existing.Mode != "" {
				mode = existing.Mode
			}
			changes = append(changes, treeChange{
				Path:   e.repoPath,
				Mode:   mode,
				Source: &contentSource{local: e.local},
			})
		}
		return changes, nil
	}

	describe := func(index map[string]remoteEntry) string {
		if strings.TrimSpace(message) != "" {
			return message
		}
		added := 0
		for _, e := range entries {
			if _, exists := index[e.repoPath]; !exists {
				added++
			}
		}
		if len(entries) == 1 {
			if added == 1 {
				return "Add " + entries[0].repoPath
			}
			return "Update " + entries[0].repoPath
		}
		if added == len(entries) {
			return fmt.Sprintf("Add %d files", len(entries))
		}
		return fmt.Sprintf("Update %d files", len(entries))
	}

	return a.commitChanges(owner, name, branch, describe, plan)
}
