package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gralliry/RepoExplorer/internal/githubutil"
	"github.com/gralliry/RepoExplorer/internal/repopath"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const downloadWorkers = 8

type Progress struct {
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Current string `json:"current"`
	Failed  int    `json:"failed"`
}

type DownloadResult struct {
	Downloaded int      `json:"downloaded"`
	Failed     int      `json:"failed"`
	Errors     []string `json:"errors"`
}

func downloadOne(ctx context.Context, client *http.Client, base, token, dest, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+githubutil.EscapeRef(path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("网络错误：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := repopath.SafeJoin(dest, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("创建子目录失败：%w", err)
	}

	file, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("创建文件失败：%w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("写入文件失败：%w", err)
	}
	return nil
}

// DownloadFiles fetches the selected repo-relative paths into dest, keeping the
// directory structure and emitting "download-progress" events as it goes.
// Authentication comes from the stored credentials.
func (githubProvider) DownloadFiles(a *App, repo, gitRef, dest string, paths []string) (*DownloadResult, error) {
	owner, name, err := githubutil.ParseRepo(repo)
	if err != nil {
		return nil, err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return nil, fmt.Errorf("未选择下载目录")
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("没有需要下载的文件")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, fmt.Errorf("无法创建下载目录：%w", err)
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	client := newHTTPClient(300 * time.Second)
	token := a.effectiveToken()
	base := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", owner, name, githubutil.EscapeRef(gitRef))

	total := len(paths)
	var done int64
	var failed int64
	var mu sync.Mutex
	var problems []string

	jobs := make(chan string)
	var wg sync.WaitGroup

	for i := 0; i < downloadWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if err := downloadOne(ctx, client, base, token, dest, path); err != nil {
					atomic.AddInt64(&failed, 1)
					mu.Lock()
					problems = append(problems, fmt.Sprintf("%s → %v", path, err))
					mu.Unlock()
				}
				current := atomic.AddInt64(&done, 1)
				runtime.EventsEmit(ctx, "download-progress", Progress{
					Done:    int(current),
					Total:   total,
					Current: path,
					Failed:  int(atomic.LoadInt64(&failed)),
				})
			}
		}()
	}

	for _, path := range paths {
		jobs <- path
	}
	close(jobs)
	wg.Wait()

	failedCount := int(atomic.LoadInt64(&failed))
	return &DownloadResult{
		Downloaded: total - failedCount,
		Failed:     failedCount,
		Errors:     problems,
	}, nil
}
