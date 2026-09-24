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
	// "owner" (yours), "collaborator" (someone else's repo you can push to) or
	// "organization" (owned by an organisation you belong to).
	Category string `json:"category"`
}

type githubRepo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	UpdatedAt     string `json:"updated_at"`
	Language      string `json:"language"`
	Owner         struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"owner"`
}

const reposPerPage = 100
const reposMaxPages = 5 // 500 repositories is plenty for a picker

// ListMyRepos returns everything the signed-in user can reach, most recently
// updated first, tagged with where it came from:
//
//   - owner         repositories they own
//   - collaborator  someone else's repository they were added to
//   - organization  a repository of an organisation they belong to
//
// Requires authentication.
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

	// Needed to tell "mine" apart from "someone else's".
	var me struct {
		Login string `json:"login"`
	}
	if err := apiGet(ctx, client, apiBase+"/user", token, &me); err != nil {
		return nil, err
	}

	out := make([]RepoSummary, 0, reposPerPage)
	for page := 1; page <= reposMaxPages; page++ {
		url := fmt.Sprintf(
			"%s/user/repos?per_page=%d&page=%d&sort=updated&affiliation=owner,collaborator,organization_member",
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

			category := "collaborator"
			switch {
			case me.Login != "" && strings.EqualFold(r.Owner.Login, me.Login):
				category = "owner"
			case strings.EqualFold(r.Owner.Type, "Organization"):
				category = "organization"
			}

			out = append(out, RepoSummary{
				FullName:      r.FullName,
				Private:       r.Private,
				Description:   strings.TrimSpace(r.Description),
				DefaultBranch: r.DefaultBranch,
				UpdatedAt:     r.UpdatedAt,
				Language:      r.Language,
				Category:      category,
			})
		}

		if len(batch) < reposPerPage {
			break
		}
	}

	return out, nil
}
