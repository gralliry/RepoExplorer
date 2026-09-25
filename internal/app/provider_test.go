package app

import "testing"

func TestGitHubProviderCanHandle(t *testing.T) {
	p := githubProvider{}

	accepted := []string{
		"gralliry/ibooks",
		"https://github.com/gralliry/ibooks",
		"git@github.com:gralliry/ibooks.git",
	}
	for _, repo := range accepted {
		if !p.CanHandle(repo) {
			t.Errorf("GitHub provider should handle %q", repo)
		}
	}

	rejected := []string{
		"https://gitlab.com/group/project.git",
		"https://gitee.com/user/project.git",
		"git@gitlab.com:group/project.git",
	}
	for _, repo := range rejected {
		if p.CanHandle(repo) {
			t.Errorf("GitHub provider must not claim non-GitHub URL %q", repo)
		}
	}
}
