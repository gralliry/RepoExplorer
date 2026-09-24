package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RepoSummary is one entry in the "my repositories" picker.
type RepoSummary struct {
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
	DefaultBranch string `json:"defaultBranch"`
	UpdatedAt     string `json:"updatedAt"`
	Language      string `json:"language"`
}

type githubRepo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	UpdatedAt     string `json:"updated_at"`
	Language      string `json:"language"`
}

const reposPerPage = 100
const reposMaxPages = 5 // 500 repositories is plenty for a picker

// ListMyRepos returns the repositories owned by the signed-in user, most
// recently updated first. Requires authentication.
//
// affiliation=owner matters: without it GitHub also returns repositories you
// merely collaborate on (someone else's repo) and repositories of every
// organisation you belong to, which is not what "my repositories" means.
func (a *App) ListMyRepos() ([]RepoSummary, error) {
	token := a.effectiveToken()
	if token == "" {
		return nil, fmt.Errorf("需要先登录 GitHub 才能列出你的仓库")
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	client := newHTTPClient(60 * time.Second)

	out := make([]RepoSummary, 0, reposPerPage)
	for page := 1; page <= reposMaxPages; page++ {
		url := fmt.Sprintf(
			"%s/user/repos?per_page=%d&page=%d&sort=updated&affiliation=owner",
			apiBase, reposPerPage, page,
		)

		var batch []githubRepo
		if err := apiGet(ctx, client, url, token, &batch); err != nil {
			return nil, err
		}

		for _, r := range batch {
			if strings.TrimSpace(r.FullName) == "" {
				continue
			}
			out = append(out, RepoSummary{
				FullName:      r.FullName,
				Private:       r.Private,
				Description:   strings.TrimSpace(r.Description),
				DefaultBranch: r.DefaultBranch,
				UpdatedAt:     r.UpdatedAt,
				Language:      r.Language,
			})
		}

		if len(batch) < reposPerPage {
			break
		}
	}

	return out, nil
}
