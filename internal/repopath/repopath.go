package repopath

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CleanPath normalises a repository-relative path and rejects anything that
// could escape the repository (absolute paths, "..", empty segments).
func CleanPath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.Trim(p, "/")
	if p == "" {
		return "", fmt.Errorf("路径不能为空")
	}
	parts := strings.Split(p, "/")
	for _, seg := range parts {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("非法路径：%s", p)
		}
	}
	return strings.Join(parts, "/"), nil
}

// CleanDir is like CleanPath but allows "" (the repository root).
func CleanDir(dir string) (string, error) {
	dir = strings.Trim(strings.ReplaceAll(strings.TrimSpace(dir), "\\", "/"), "/")
	if dir == "" {
		return "", nil
	}
	return CleanPath(dir)
}

func Join(base, rel string) string {
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

// SafeJoin joins a repo-relative path onto dest, refusing anything that tries
// to escape the destination directory.
func SafeJoin(dest, rel string) (string, error) {
	parts := strings.Split(rel, "/")
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		switch p {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("路径包含 ..，已跳过")
		}
		cleaned = append(cleaned, p)
	}
	if len(cleaned) == 0 {
		return "", fmt.Errorf("非法路径：%s", rel)
	}
	return filepath.Join(append([]string{dest}, cleaned...)...), nil
}
