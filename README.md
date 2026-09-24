# RepoExplorer

把 GitHub 仓库当成一个**资源管理器里的文件夹**来用的桌面工具。打开仓库后，界面和你熟悉的
Windows 文件管理器一样：地址栏、双击进入文件夹、多选、拖拽移动、F2 重命名、右键菜单。
区别只是所有改动都直接提交到远端仓库 —— 每次操作 = **一个提交**。

技术栈：**Go + Wails v2**，前端是无构建的原生 HTML/CSS/JS。

## 界面

和 Windows 资源管理器一致的操作：

| 操作 | 行为 |
|---|---|
| 双击文件夹 | 进入该文件夹 |
| 双击文件 | 打开内置编辑器 |
| 单击 | 选中（只选中，不会打开） |
| Ctrl + 单击 | 加选 / 取消 |
| Shift + 单击 | 选中一段区间 |
| 单击空白处 | 取消选择 |
| 拖拽文件到文件夹上 | 移动到该文件夹 |
| 拖到空白处 | 移动到当前文件夹 |
| 地址栏 | 面包屑导航；点空白处可输入完整路径 |
| ← → ↑ | 后退 / 前进 / 上一级 |
| 列标题 | 按名称 / 大小 / 类型排序 |
| 「详细信息 / 图标」 | 切换列表视图和图标视图 |
| 状态栏 | 对象数、已选数量与大小、下载目录、下载进度 |

键盘快捷键：`F2` 重命名、`Delete` 删除、`Enter` 打开、`Backspace` 上一级、
`F5` 刷新、`Ctrl+A` 全选、`Alt+←/→` 后退 / 前进、`Esc` 取消。

## 功能

**读**

- 输入 `owner/repo`、完整 GitHub 链接或 `git@github.com:owner/repo.git`
- 自动获取默认分支与分支列表，可切换分支
- 浏览任意深度的目录，每项显示大小 / 项数 / 类型
- 搜索框可**递归整个仓库**查找文件
- 选中文件或文件夹下载到本地，保留目录结构（8 并发、状态栏显示进度）
- 支持私有仓库：OAuth 登录，或手动填 Token
- 遵循 Windows 系统代理设置

**写**（需要登录，**每次操作 = 一个提交**）

- 新建文件 / 新建文件夹
- 编辑文本文件，保存即提交
- 上传本地文件或整个文件夹（递归，自动跳过 `.git`）
- 重命名（F2 就地改名，和资源管理器一样自动选中不含扩展名的部分）
- 移动到别的目录，支持整个文件夹
- 删除文件 / 文件夹
- 破坏性操作前会列出**将受影响的所有文件**再确认
- commit message 自动生成（`Add src/a.go`、`Rename docs to source`、`Delete 3 files`…）

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
wails build    # 生成 build\bin\RepoExplorer.exe
```

前端是**无构建**的：`frontend/dist/` 里就是普通 HTML/CSS/JS，
**没有 npm、没有 node_modules、没有打包器**，改完直接 `wails build`。

## 测试

```powershell
go test ./...                                    # 纯逻辑测试，不联网
$env:LIVE_GITHUB=1; go test -run Live -v ./...   # 真实访问 GitHub API 的集成测试
```

只读集成测试会真的去拉 `octocat/Hello-World` 的目录树、真的下载一个文件，
并验证下载不存在的文件会返回 404。

写操作测试需要**一个可以随便改的仓库**，它们会真实地创建、重命名、移动、删除文件：

```powershell
gh repo create RepoExplorer-scratch --private --add-readme
$env:GITHUB_TOKEN     = (gh auth token)
$env:GITHUB_TEST_REPO = "you/RepoExplorer-scratch"
go test -run LiveWrite -v ./...
gh repo delete you/RepoExplorer-scratch --yes
```

覆盖的流程：新建 → 读回 → 更新 → 原地重命名 → 上传本地文件夹 → 移动整个文件夹
→ 删除；并且断言「移动一个 2 文件的文件夹只产生 1 个提交」（原子性）。
另外还验证了移动到已存在的路径会被拒绝、含 `..` 的路径会被拒绝、未登录时写操作会报错。

## 使用步骤

0. 点右上角「认证」登录 GitHub。**读公开仓库不需要登录，任何写操作都需要。**
   见下方「GitHub 认证」。
1. 点「打开仓库」弹出选择器：
   - 上面是**你自己的仓库**，按「我的仓库 / 组织仓库 / 协作仓库」分组（点分组标题可收起），
     带搜索框，点一下就直接打开
   - 下面是**公开仓库**：手填 `owner/repo` 或完整 URL，可打开任意公开仓库
2. 双击文件夹进入；用 ← → ↑ 或地址栏面包屑导航；「详细信息 / 图标」切换视图
3. 选中文件后点「下载」。第一次会让你挑本地目录，之后点状态栏右侧的「下载到 …」可随时改
4. 右键文件 / 文件夹，或用工具栏的「新建 ▾」「上传 ▾」
5. 双击文本文件编辑，保存会在当前分支上产生一个提交

### 只读仓库

写权限是程序用 `GET /repos/{owner}/{repo}` 返回的 `permissions.push` **实际判断**的，不是靠"你是从哪个入口打开的"来猜：

- 自己的仓库 → 可写，功能齐全
- 别人的公开仓库 → **只读**：只能浏览和下载，「新建 / 上传」按钮会禁用，右键菜单里的
  写操作也会置灰，状态栏显示「只读（只能下载）」
- 没登录 → 只读

## 项目结构

```
RepoExplorer/
├─ main.go             wails.Run 配置、嵌入 frontend/dist
├─ app.go              App 结构体 + PickFolder（目录选择）
├─ auth.go             GitHub 认证：OAuth Device Flow、凭据读写
├─ github.go           GitHub API（读）：解析仓库、分支、目录树
├─ gitdata.go          GitHub API（写）：blob / tree / commit / ref
├─ mutate.go           写操作：新建、编辑、上传、重命名、移动、删除
├─ download.go         并发下载 + 进度事件
├─ httpclient.go       HTTP 客户端（跟随系统代理）
├─ proxy_windows.go    读注册表里的 WinINET 代理设置
├─ proxy_other.go      其它平台的空实现
├─ github_test.go      仓库解析、路径转义等单元测试
├─ mutate_test.go      路径校验、本地文件夹展开等单元测试
├─ auth_test.go        凭据读写测试
├─ integration_test.go 只读的真实网络集成测试（需 LIVE_GITHUB=1）
├─ write_live_test.go  写操作的真实网络集成测试（需 GITHUB_TOKEN + GITHUB_TEST_REPO）
├─ wails.json
├─ frontend/
│  ├─ dist/            index.html / styles.css / main.js —— 前端源码，直接嵌入
│  └─ wailsjs/         Wails 自动生成的绑定（已 gitignore）
└─ build/              appicon.png / windows/ / bin/（编译产物）
```

## 写操作是怎么实现的

没有用 Contents API，而是走 **Git Data API**，因为前者一次只能改一个文件：
重命名一个 200 文件的文件夹会变成 200 次「新建 + 删除」、200 个提交，做到一半失败还会留下残局。

现在的流程是：

1. 读当前分支头 → commit sha → 根 tree sha（并列出全部文件及其 blob sha）
2. 新内容 `POST /git/blobs`
3. 用 `base_tree` 把改动叠加成新 tree：新增/修改给新 blob，删除给 `sha: null`，
   **移动只是把新路径指向同一个 blob sha**，不重复上传内容
4. `POST /git/commits`（父提交 = 第 1 步读到的头）
5. `PATCH /git/refs/heads/{branch}` 把分支指过去

所以不管一次操作涉及多少文件，**永远只产生一个提交**，要么全成功要么全不动。
如果中途有人推了新提交，第 5 步会因为不是快进而被 GitHub 拒绝，程序会提示你重新加载。

三个必须知道的限制：

- **Git 存不了空文件夹**。所以「新建文件夹」会要求你顺手建里面的第一个文件。
- **每次写操作就是一次真实提交**，直接落在你当前选中的分支上（不是 PR）。
  删除之后只能去 git 历史里找回来。
- 写操作会先列出受影响的文件让你确认，涉及几百个文件时列表会截断显示（只影响展示）。

## GitHub 认证

有两种方式，在界面右上角的「认证」按钮里切换。

### 方式一：GitHub OAuth 登录（推荐）

用的是 **Device Flow**（设备码流程），所以**只需要 client_id，不需要 client secret**，
代码里也没有任何密钥，可以安全地分发这个程序。

程序**已经内置了一个 client_id**，开箱即用：点「认证」→「用 GitHub 登录」，
浏览器会自动打开授权页，把界面上显示的验证码填进去即可。
申请的权限是 `repo`（能访问私有仓库）。

想换掉这个内置的 client_id，只能在构建时替换 —— 界面上**没有**让用户填写 client_id 的入口：

```powershell
wails build -ldflags "-X main.defaultClientID=Ov23li..."
```

如果要自己注册一个（免费，两分钟）：

1. 打开 <https://github.com/settings/applications/new>
2. 填写：

   | 字段 | 填什么 |
   |---|---|
   | Application name | 随便，例如 `RepoExplorer` |
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
%APPDATA%\RepoExplorer\auth.json
```

里面是明文（`token` / `tokenSource` / `login`），删掉这个文件就等于退出登录。
点「退出 / 清除凭据」也会清掉它。注入用的 client_id 是编译进程序的常量，不保存在这里。

## 前后端接口

前端直接调用 Wails 注入的 `window.go.main.App.*`，不依赖生成的 ES module
绑定文件，所以不需要任何打包器。

**读 / 下载**

| 前端调用 | 作用 |
|---|---|
| `FetchRepoTree(repo, ref)` | 解析仓库并拉取整棵目录树 |
| `ReadFile(repo, ref, path)` | 读取文本文件内容（编辑器用） |
| `DownloadFiles(repo, ref, dest, paths)` | 并发下载选中的文件 |

**写**（每个调用 = 一个提交）

| 前端调用 | 作用 |
|---|---|
| `SaveFile(repo, ref, path, content, msg)` | 新建或覆盖一个文件 |
| `DeletePaths(repo, ref, paths, msg)` | 删除若干文件 |
| `MovePaths(repo, ref, moves, msg)` | 重命名 / 移动（`moves` 是 `{from,to}` 列表） |
| `PlanUpload(dir, localPaths)` | 预览上传会写入哪些路径（不提交） |
| `UploadFiles(repo, ref, dir, localPaths, msg)` | 上传本地文件 / 文件夹 |

**对话框与认证**

| 前端调用 | 作用 |
|---|---|
| `PickFolder()` | 选择下载目录 |
| `PickUploadFiles()` / `PickUploadFolder()` | 选择要上传的本地文件 / 文件夹 |
| `GetAuth()` | 读取当前认证状态 |
| `StartGitHubLogin()` / `CancelGitHubLogin()` | 开始 / 取消 Device Flow 登录 |
| `SaveManualToken(token)` / `ClearAuth()` | 保存手动 Token / 清除凭据 |
| `OpenExternal(url)` | 用系统浏览器打开 https 链接 |

后台通过事件回传进度：

| 事件 | 何时发出 |
|---|---|
| `download-progress` | 每下载完一个文件 |
| `github-login-code` | 拿到设备码，界面显示验证码 |
| `github-login-done` | 授权成功，已保存凭据 |
| `github-login-error` | 登录失败 / 取消 / 超时 |

## 实现说明

- **目录数据**：`GET /repos/{owner}/{repo}/git/trees/{ref}?recursive=1` 一次拿到
  全量文件列表（扁平）。前端把它缓存下来，每进入一个文件夹就从这份列表里筛出该层，
  所以浏览目录不再产生任何网络请求（刷新时才重新拉取）。
- **下载**：走 `raw.githubusercontent.com`，8 个 worker goroutine 并发，
  写入时保留原有相对路径。
- **路径安全**：下载用 `safeJoin` 拒绝含 `..` 的条目，防止写到目标目录之外；
  写操作用 `cleanRepoPath` 拒绝 `..`、绝对路径和空路径段。
- **代理**：Go 默认只认 `HTTP_PROXY` / `HTTPS_PROXY` 环境变量，
  `proxy_windows.go` 额外读取注册表里的系统代理设置，让程序跟随浏览器。
- **截断**：GitHub 对超大仓库（约 10 万条目以上）会截断返回，界面会给出提示；
  写操作遇到截断会直接拒绝，避免误删。

## 已知限制

- 只支持 GitHub 的公开/私有仓库。
- 分支下拉只列出分支，不含 tag / 具体 commit。
- 写操作直接提交到当前分支（不建 PR），且**没有撤销按钮**，删错了只能去 git 历史里找。
- 内置编辑器只处理 UTF-8 文本文件，上限 2 MiB；更大的或二进制文件请用「下载」。
- 内置的 client_id 属于作者自己注册的 OAuth App；如果要对公众分发，
  建议换成自己的（见「GitHub 认证」）。
