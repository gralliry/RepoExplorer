package app

import "fmt"

// gitProvider is the host adapter boundary. App methods exposed to Wails should
// delegate through this interface instead of calling a host API directly.
type gitProvider interface {
	ID() string
	CanHandle(repo string) bool

	FetchRepoTree(a *App, repo, gitRef string) (*RepoTree, error)
	DownloadFiles(a *App, repo, gitRef, dest string, paths []string) (*DownloadResult, error)
	OpenFile(a *App, repo, branch, repoPath string) (string, error)
	ReadFile(a *App, repo, branch, filePath string) (*FileContent, error)
	SaveFile(a *App, repo, branch, filePath, content, message string) (*CommitResult, error)
	DeletePaths(a *App, repo, branch string, paths []string, message string) (*CommitResult, error)
	MovePaths(a *App, repo, branch string, moves []PathMove, message string) (*CommitResult, error)
	CopyPaths(a *App, repo, branch string, copies []PathMove, message string) (*CommitResult, error)
	UploadFiles(a *App, repo, branch, repoDir string, localPaths []string, message string) (*CommitResult, error)
}

var gitProviders = []gitProvider{githubProvider{}}

func (a *App) providerForRepo(repo string) (gitProvider, error) {
	for _, p := range gitProviders {
		if p.CanHandle(repo) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("暂不支持这个 Git 仓库地址：%s", repo)
}

func (a *App) FetchRepoTree(repo, gitRef string) (*RepoTree, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return nil, err
	}
	return provider.FetchRepoTree(a, repo, gitRef)
}

func (a *App) ListMyRepos() ([]RepoSummary, error) {
	return githubProvider{}.ListMyRepos(a)
}

func (a *App) DownloadFiles(repo, gitRef, dest string, paths []string) (*DownloadResult, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return nil, err
	}
	return provider.DownloadFiles(a, repo, gitRef, dest, paths)
}

func (a *App) OpenFile(repo, branch, repoPath string) (string, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return "", err
	}
	return provider.OpenFile(a, repo, branch, repoPath)
}

func (a *App) ReadFile(repo, branch, filePath string) (*FileContent, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return nil, err
	}
	return provider.ReadFile(a, repo, branch, filePath)
}

func (a *App) SaveFile(repo, branch, filePath, content, message string) (*CommitResult, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return nil, err
	}
	return provider.SaveFile(a, repo, branch, filePath, content, message)
}

func (a *App) DeletePaths(repo, branch string, paths []string, message string) (*CommitResult, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return nil, err
	}
	return provider.DeletePaths(a, repo, branch, paths, message)
}

func (a *App) MovePaths(repo, branch string, moves []PathMove, message string) (*CommitResult, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return nil, err
	}
	return provider.MovePaths(a, repo, branch, moves, message)
}

func (a *App) CopyPaths(repo, branch string, copies []PathMove, message string) (*CommitResult, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return nil, err
	}
	return provider.CopyPaths(a, repo, branch, copies, message)
}

func (a *App) UploadFiles(repo, branch, repoDir string, localPaths []string, message string) (*CommitResult, error) {
	provider, err := a.providerForRepo(repo)
	if err != nil {
		return nil, err
	}
	return provider.UploadFiles(a, repo, branch, repoDir, localPaths, message)
}
