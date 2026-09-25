package githubutil

import (
	"fmt"
	"strings"
)

// ParseRepo accepts "owner/repo", a full GitHub URL, or
// "git@github.com:owner/repo.git" and returns the owner and repository name.
func ParseRepo(input string) (string, string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", fmt.Errorf("请输入仓库，例如 owner/repo")
	}

	for _, prefix := range []string{"https://", "http://", "ssh://"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	s = strings.TrimPrefix(s, "git@")
	s = strings.ReplaceAll(s, ":", "/")
	for _, host := range []string{"github.com/", "www.github.com/"} {
		if strings.HasPrefix(s, host) {
			s = strings.TrimPrefix(s, host)
			break
		}
	}
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.Trim(s, "/")

	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' })
	if len(parts) < 2 {
		return "", "", fmt.Errorf("无法从“%s”识别仓库，请输入 owner/repo 或完整的 GitHub 链接", input)
	}
	return parts[0], parts[1], nil
}

// EscapeRef percent-encodes a ref name or a repo path for use inside a URL,
// deliberately keeping "/" readable — branch names and file paths may contain it.
func EscapeRef(s string) string {
	const unreserved = "-._~"
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || strings.ContainsRune(unreserved, r):
			b.WriteRune(r)
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			for i := 0; i < len(string(r)); i++ {
				fmt.Fprintf(&b, "%%%02X", string(r)[i])
			}
		}
	}
	return b.String()
}
