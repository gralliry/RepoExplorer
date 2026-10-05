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
		"ssh://git.example.com/group/project.git",
	}
	for _, repo := range rejected {
		if p.CanHandle(repo) {
			t.Errorf("GitHub provider must not claim non-GitHub URL %q", repo)
		}
	}
}

func TestGenericGitProviderCanHandle(t *testing.T) {
	p := genericGitProvider{}

	accepted := []string{
		"https://gitlab.com/group/project.git",
		"https://gitee.com/user/project.git",
		"ssh://git.example.com/group/project.git",
		"gitlab.com/group/project",
	}
	for _, repo := range accepted {
		if !p.CanHandle(repo) {
			t.Errorf("generic Git provider should handle %q", repo)
		}
	}

	rejected := []string{
		"",
		"https://github.com/gralliry/ibooks",
	}
	for _, repo := range rejected {
		if p.CanHandle(repo) {
			t.Errorf("generic Git provider should not claim %q", repo)
		}
	}
}
