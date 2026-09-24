# RepoDownloader

一个桌面小工具：输入 GitHub 仓库 → 自动拉取整棵目录树 → 只勾选你需要的文件/文件夹 → 下载到本地。
专门用来解决「仓库太大、只想下载其中某个文件夹」的问题。

技术栈：**Go + Wails v2**，前端是无构建的原生 HTML/CSS/JS。

## 为什么用 Wails 而不是 Tauri

同一台机器、同一个功能，实测对比：

| | Tauri (Rust) | Wails (Go) |
|---|---|---|
| 编译产物占用 | **6.5 GB**（debug + release） | **11.5 MB**（整个项目） |
| 一次 release 构建 | 3 分 21 秒 | **5.2 秒**（改一行后增量 2.7 秒） |
| 编译缓存位置 | 项目内 `target/`，**每个项目各存一份** | 全局 `%LOCALAPPDATA%\go-build`，**所有项目共享**（实测 279 MB） |
| 生成的 exe | 更小（常见的 Tauri 应用约 3–8 MB，未实测） | **11.3 MB**（含 Go runtime） |

结论：Wails 换来的是「构建几乎不占项目空间 + 秒级编译」，代价是最终 exe 更大。

## 环境要求

- Go 1.21+（本项目在 Go 1.27 上验证）
- Wails CLI v2：
  ```powershell
  go install github.com/wailsapp/wails/v2/cmd/wails@latest
  ```
- Windows 需要 WebView2 运行时（Win10/11 一般自带）

## 运行 / 打包

```powershell
wails dev      # 开发模式，改 Go 代码会自动重编译
wails build    # 生成 build\bin\RepoDownloader.exe
```

前端是**无构建**的：`frontend/dist/` 里就是普通 HTML/CSS/JS，
**没有 npm、没有 node_modules、没有打包器**，改完直接 `wails build`。

## 测试

```powershell
go test ./...                                    # 纯逻辑测试，不联网
$env:LIVE_GITHUB=1; go test -run Live -v ./...   # 真实访问 GitHub API 的集成测试
```

集成测试会真的去拉 `octocat/Hello-World` 的目录树、真的下载一个文件、
并验证下载不存在的文件会返回 404。

## 使用步骤

0. （可选）点右上角「认证」登录 GitHub —— 访问私有仓库或提高 API 配额时才需要，
   公开仓库不登录也能用。见下方「GitHub 认证」。
1. 在「仓库」里填 `owner/repo`，例如 `tauri-apps/tauri`
2. 点「加载目录」，稍等片刻会显示整棵目录树
3. 勾选想要的文件或整个文件夹（勾文件夹会连带勾中里面所有文件）
4. 点「选择…」挑一个本地目录，再点「开始下载」

## 项目结构

```
RepoDownloader/
├─ main.go            wails.Run 配置、嵌入 frontend/dist
├─ app.go             App 结构体 + PickFolder（目录选择）
├─ auth.go            GitHub 认证：OAuth Device Flow、凭据读写
├─ github.go          GitHub API：解析仓库、分支、目录树
├─ download.go        并发下载 + 进度事件
├─ httpclient.go      HTTP 客户端（跟随系统代理）
├─ proxy_windows.go   读注册表里的 WinINET 代理设置
├─ proxy_other.go     其它平台的空实现
├─ github_test.go     单元测试
├─ auth_test.go       凭据读写测试
├─ integration_test.go 真实网络集成测试（需 LIVE_GITHUB=1）
├─ wails.json
├─ frontend/
│  ├─ dist/           index.html / styles.css / main.js —— 前端源码，直接嵌入
│  └─ wailsjs/        Wails 自动生成的绑定（已 gitignore）
└─ build/             appicon.png / windows/ / bin/（编译产物）
```

## GitHub 认证

有两种方式，在界面右上角的「认证」按钮里切换。

### 方式一：GitHub OAuth 登录（推荐）

用的是 **Device Flow**（设备码流程），所以**只需要 client_id，不需要 client secret**，
代码里也没有任何密钥，可以安全地分发这个程序。

程序**已经内置了一个 client_id**，开箱即用：点「认证」→「用 GitHub 登录」，
浏览器会自动打开授权页，把界面上显示的验证码填进去即可。
申请的权限是 `repo`（能访问私有仓库）。

想换成自己的 OAuth App 也可以（可选），两种覆盖方式：

- 运行时：在「认证」弹窗里填入自己的 Client ID 并保存
- 构建时：`wails build -ldflags "-X main.defaultClientID=Ov23li..."`

如果要自己注册一个（免费，两分钟）：

1. 打开 <https://github.com/settings/applications/new>
2. 填写：

   | 字段 | 填什么 |
   |---|---|
   | Application name | 随便，例如 `RepoDownloader` |
   | Homepage URL | 随便，例如 `https://github.com/` |
   | Application description | 可留空 |
   | Authorization callback URL | 填 `http://localhost`（Device Flow 用不到，但必填） |

3. 点 **Register application**
4. 复制页面上的 **Client ID**（形如 `Ov23li…`）
5. **关键一步**：在该应用的设置页勾选 **Enable Device Flow**，再点 **Update application**
   —— 漏掉这步登录会直接报 `device_flow_disabled`
6. 回到程序的「认证」弹窗，覆盖填入这个 Client ID

### 方式二：手动填 Personal Access Token

不想注册 OAuth App 就用这个：在「认证」里粘贴一个 PAT。
私有仓库需要 `repo` 权限；只是为了提高 API 配额的话，不带任何权限的 token 也行。

### 凭据保存在哪

两种方式的凭据都保存在：

```
%APPDATA%\RepoDownloader\auth.json
```

里面是明文（`clientId` / `token` / `login`），删掉这个文件就等于退出登录。
点「退出 / 清除凭据」也会清掉 token（保留 client_id，省得重填）。

## 前后端接口

前端直接调用 Wails 注入的 `window.go.main.App.*`，不依赖生成的 ES module
绑定文件，所以不需要任何打包器。

| 前端调用 | Go 方法 |
|---|---|
| `FetchRepoTree(repo, ref)` | 解析仓库并拉取整棵目录树 |
| `DownloadFiles(repo, ref, dest, paths)` | 并发下载选中的文件 |
| `PickFolder()` | 打开系统「选择目录」对话框 |
| `GetAuth()` | 读取当前认证状态 |
| `SetClientID(id)` | 保存 OAuth App 的 client id |
| `StartGitHubLogin()` | 开始 Device Flow 登录 |
| `CancelGitHubLogin()` | 取消等待中的登录 |
| `SaveManualToken(token)` | 保存手动填写的 PAT |
| `ClearAuth()` | 清除已保存的凭据 |
| `OpenExternal(url)` | 用系统浏览器打开 https 链接 |

认证相关的进度通过事件回传：

| 事件 | 何时发出 |
|---|---|
| `download-progress` | 每下载完一个文件 |
| `github-login-code` | 拿到设备码，界面显示验证码 |
| `github-login-done` | 授权成功，已保存凭据 |
| `github-login-error` | 登录失败 / 取消 / 超时 |


## 实现说明

- **目录树**：`GET /repos/{owner}/{repo}/git/trees/{ref}?recursive=1` 一次拿到
  全量文件列表（扁平），前端再还原成树。
- **下载**：走 `raw.githubusercontent.com`，8 个 worker goroutine 并发，
  写入时保留原有相对路径。
- **路径安全**：`safeJoin` 会拒绝含 `..` 的条目，防止写到目标目录之外。
- **代理**：Go 默认只认 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量，
  `proxy_windows.go` 额外读取注册表里的系统代理设置，让程序跟随浏览器。
- **截断**：GitHub 对超大仓库（约 10 万条目以上）会截断返回，界面会给出提示。

## 已知限制

- 只支持 GitHub 的公开/私有仓库。
- 分支下拉只列出分支，不含 tag / 具体 commit。
- 内置的 client_id 属于本机作者自己注册的 OAuth App；如果你要对外分发，
  建议换成自己的（见「GitHub 认证」）。
