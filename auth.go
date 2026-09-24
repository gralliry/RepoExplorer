package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	deviceCodeURL  = "https://github.com/login/device/code"
	accessTokenURL = "https://github.com/login/oauth/access_token"
	// "repo" is what lets the downloader reach private repositories.
	oauthScope = "repo"
)

// defaultClientID is the OAuth App the app always uses. A client id is *not* a
// secret — the device flow needs no client secret — so shipping it inside the
// binary is safe.
//
// End users deliberately cannot change it: there is no UI for it. Replace it at
// build time instead:
//
//	wails build -ldflags "-X main.defaultClientID=Ov23li..."
var defaultClientID = "Ov23li65G34wRLNaxbvB"

// authConfig is persisted to %APPDATA%\RepoDownloader\auth.json
type authConfig struct {
	Token       string `json:"token"`
	TokenSource string `json:"tokenSource"` // "oauth" | "manual"
	Login       string `json:"login"`
}

// AuthInfo is the authentication state the UI renders.
type AuthInfo struct {
	LoggedIn bool   `json:"loggedIn"`
	Login    string `json:"login"`
	Source   string `json:"source"`
}

// LoginCode carries the device-flow code the user has to type in the browser.
type LoginCode struct {
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
}

// LoginResult is emitted once the device flow succeeds.
type LoginResult struct {
	Login string `json:"login"`
}

func authConfigPath() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		if dir, err := os.UserConfigDir(); err == nil {
			base = dir
		} else {
			base = "."
		}
	}
	return filepath.Join(base, "RepoDownloader", "auth.json")
}

func (a *App) loadAuth() {
	data, err := os.ReadFile(authConfigPath())
	if err != nil {
		return
	}
	var cfg authConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	a.authMu.Lock()
	a.auth = cfg
	a.authMu.Unlock()
}

// saveAuthLocked persists the config. Callers must hold a.authMu.
func (a *App) saveAuthLocked() error {
	path := authConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败：%w", err)
	}
	data, err := json.MarshalIndent(a.auth, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("写入配置失败：%w", err)
	}
	return nil
}

// effectiveToken is what the GitHub API and the downloader authenticate with.
func (a *App) effectiveToken() string {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	return strings.TrimSpace(a.auth.Token)
}

/* ------------------------------------------------------------ bound methods */

// GetAuth reports the current authentication state.
func (a *App) GetAuth() AuthInfo {
	a.authMu.Lock()
	defer a.authMu.Unlock()

	source := a.auth.TokenSource
	if source == "" && a.auth.Token != "" {
		source = "manual"
	}
	return AuthInfo{
		LoggedIn: a.auth.Token != "",
		Login:    a.auth.Login,
		Source:   source,
	}
}

// SaveManualToken stores a personal access token as an alternative to logging in.
func (a *App) SaveManualToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("Token 不能为空")
	}
	a.authMu.Lock()
	defer a.authMu.Unlock()
	a.auth.Token = token
	a.auth.TokenSource = "manual"
	a.auth.Login = ""
	return a.saveAuthLocked()
}

// ClearAuth forgets the stored token (keeps the client id).
func (a *App) ClearAuth() error {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	a.auth.Token = ""
	a.auth.TokenSource = ""
	a.auth.Login = ""
	return a.saveAuthLocked()
}

// OpenExternal opens an https URL in the user's browser.
func (a *App) OpenExternal(rawURL string) error {
	if !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("只允许打开 https 链接")
	}
	runtime.BrowserOpenURL(a.ctx, rawURL)
	return nil
}

// StartGitHubLogin kicks off the OAuth device flow in the background. Progress
// is reported through the github-login-* events.
func (a *App) StartGitHubLogin() error {
	if strings.TrimSpace(defaultClientID) == "" {
		return fmt.Errorf("程序没有内置 client_id，请用 -ldflags 指定后再构建")
	}

	a.cancelLogin()
	ctx, cancel := context.WithCancel(context.Background())
	a.loginMu.Lock()
	a.loginCancel = cancel
	a.loginMu.Unlock()

	go a.runDeviceFlow(ctx, defaultClientID)
	return nil
}

// CancelGitHubLogin stops a login that is waiting for the user.
func (a *App) CancelGitHubLogin() {
	a.cancelLogin()
}

func (a *App) cancelLogin() {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.loginCancel != nil {
		a.loginCancel()
		a.loginCancel = nil
	}
}

/* --------------------------------------------------------------- device flow */

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Error           string `json:"error"`
	ErrorDesc       string `json:"error_description"`
}

type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
	Interval    int    `json:"interval"`
}

func postForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("网络请求失败：%w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败：%w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GitHub 返回 HTTP %d：%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析响应失败：%w", err)
	}
	return nil
}

func requestDeviceCode(ctx context.Context, client *http.Client, clientID string) (*deviceCodeResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", oauthScope)

	var out deviceCodeResponse
	if err := postForm(ctx, client, deviceCodeURL, form, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("申请验证码失败：%s %s", out.Error, out.ErrorDesc)
	}
	if out.DeviceCode == "" {
		return nil, fmt.Errorf("GitHub 没有返回设备码")
	}
	return &out, nil
}

func pollAccessToken(ctx context.Context, client *http.Client, clientID, deviceCode string) (*accessTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	var out accessTokenResponse
	if err := postForm(ctx, client, accessTokenURL, form, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func fetchLogin(ctx context.Context, client *http.Client, token string) string {
	var user struct {
		Login string `json:"login"`
	}
	if err := apiGet(ctx, client, apiBase+"/user", token, &user); err != nil {
		return ""
	}
	return user.Login
}

func (a *App) runDeviceFlow(ctx context.Context, clientID string) {
	client := newHTTPClient(30 * time.Second)

	code, err := requestDeviceCode(ctx, client, clientID)
	if err != nil {
		a.emitLoginError(err)
		return
	}

	runtime.EventsEmit(a.ctx, "github-login-code", LoginCode{
		UserCode:        code.UserCode,
		VerificationURI: code.VerificationURI,
		ExpiresIn:       code.ExpiresIn,
	})
	// Open the verification page straight away — the code stays visible in the
	// UI in case the browser does not open or the user switches away.
	runtime.BrowserOpenURL(a.ctx, code.VerificationURI)

	interval := code.Interval
	if interval < 5 {
		interval = 5
	}
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(interval) * time.Second):
		}

		resp, err := pollAccessToken(ctx, client, clientID, code.DeviceCode)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			a.emitLoginError(err)
			return
		}

		if resp.AccessToken != "" {
			login := fetchLogin(ctx, client, resp.AccessToken)
			a.authMu.Lock()
			a.auth.Token = resp.AccessToken
			a.auth.TokenSource = "oauth"
			a.auth.Login = login
			saveErr := a.saveAuthLocked()
			a.authMu.Unlock()

			if saveErr != nil {
				a.emitLoginError(saveErr)
				return
			}
			runtime.EventsEmit(a.ctx, "github-login-done", LoginResult{Login: login})
			return
		}

		switch resp.Error {
		case "authorization_pending":
			// keep waiting
		case "slow_down":
			interval += 5
		case "access_denied":
			a.emitLoginError(fmt.Errorf("你取消了授权"))
			return
		case "expired_token":
			a.emitLoginError(fmt.Errorf("验证码已过期，请重新登录"))
			return
		default:
			a.emitLoginError(fmt.Errorf("登录失败：%s %s", resp.Error, resp.ErrorDesc))
			return
		}
	}

	a.emitLoginError(fmt.Errorf("登录超时，请重新尝试"))
}

func (a *App) emitLoginError(err error) {
	runtime.EventsEmit(a.ctx, "github-login-error", err.Error())
}
