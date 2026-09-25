package app

import "strings"

type githubProvider struct{}

func (githubProvider) ID() string { return "github" }

func (githubProvider) CanHandle(repo string) bool {
	s := strings.TrimSpace(strings.ToLower(repo))
	if s == "" {
		return false
	}
	if strings.Contains(s, "github.com") || strings.HasPrefix(s, "git@github.com:") {
		return true
	}
	if strings.Contains(s, "://") || strings.HasPrefix(s, "git@") {
		return false
	}
	return strings.Count(strings.Trim(s, "/"), "/") >= 1
}
