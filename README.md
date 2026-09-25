# RepoExplorer

RepoExplorer 是一个桌面端 Git 仓库文件浏览与管理工具。它把远端仓库呈现成类似 Windows 资源管理器的界面：可以浏览目录、搜索文件、下载、上传、新建、重命名、移动、复制、剪切、粘贴和删除。

当前已实现 GitHub Provider；后端已预留通用 Git Provider 适配层，后续可继续扩展 GitLab、Gitee 或其它 Git 服务。

## 特性

- 类资源管理器交互：面包屑地址栏、后退/前进/上一级、列表排序、右键菜单
- 支持公开仓库只读浏览
- 支持 GitHub OAuth Device Flow 登录
- 支持 Personal Access Token 登录
- 登录后可加载个人仓库、组织仓库和协作仓库
- 支持选择模式：点击“选择”后出现复选框，后续操作基于选中项目
- 支持下载文件/文件夹，保留目录结构，并显示任务进度
- 支持上传文件和文件夹；上传文件夹会保留最外层文件夹名
- 支持复制、剪切、粘贴、移动、重命名、删除
- 写操作使用 Git Data API，每次操作生成一个原子提交
- 错误提示会保持显示，点击关闭才消失，且文本可复制
- 前端无构建步骤：原生 HTML/CSS/JavaScript

## 当前支持范围

| 能力 | 状态 |
|---|---|
| GitHub 公开仓库浏览 | 已支持 |
| GitHub 私有仓库浏览 | 已支持，需要登录 |
| GitHub 写操作 | 已支持，需要写权限 |
| GitLab / Gitee / 其它 Git 服务 | 已预留 Provider 架构，尚未实现 |
| 本地 Git 仓库 | 尚未实现 |

> 说明：输入裸 `owner/repo` 时默认按 GitHub 仓库处理；非 GitHub URL 当前会提示暂不支持。

## 技术栈

- Go
- Wails v2
- 原生 HTML/CSS/JavaScript
- GitHub REST API / Git Data API

## 环境要求

- Go 1.21+（当前项目使用 Go 1.25+ 配置，开发环境在 Go 1.27 验证）
- Wails CLI v2
- Windows WebView2 Runtime（Windows 10/11 通常已自带）

安装 Wails CLI：

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

## 运行

```powershell
wails dev
```

开发模式会启动桌面窗口。前端位于 `frontend/dist/`，无需 npm、无需打包。

## 构建

```powershell
wails build
```

构建产物：

```text
build/bin/RepoExplorer.exe
```

## 测试

运行全部非联网测试：

```powershell
go test ./...
```

检查前端脚本语法：

```powershell
node --check frontend/dist/main.js
```

运行只读联网测试：

```powershell
$env:LIVE_GITHUB=1
go test -run Live -v ./...
```

运行真实写操作测试需要准备一个可随意修改的仓库：

```powershell
gh repo create RepoExplorer-scratch --private --add-readme
$env:GITHUB_TOKEN     = (gh auth token)
$env:GITHUB_TEST_REPO = "you/RepoExplorer-scratch"
go test -run LiveWrite -v ./...
gh repo delete you/RepoExplorer-scratch --yes
```

## 使用说明

### 打开仓库

1. 点击“打开仓库”
2. 已登录时可在“我的仓库”列表中选择仓库
3. 未登录时可手动输入公开仓库，例如：
   - `owner/repo`
   - `https://github.com/owner/repo`
   - `git@github.com:owner/repo.git`

### 登录

点击右上角设置按钮进入账号设置。登录方式二选一：

- GitHub 登录：推荐方式，使用 OAuth Device Flow
- Token 登录：手动填写 Personal Access Token

两种方式不需要同时填写。

### 选择和操作

- 默认单击项目会选中
- 点击顶栏“选择”进入选择模式，项目前会出现复选框
- 再次点击“取消”退出选择模式并清空选择
- 后续操作基于当前选中的项目

顶栏和右键菜单都提供常用操作：

- 复制
- 剪切
- 粘贴
- 重命名
- 删除
- 下载

### 快捷键

| 快捷键 | 功能 |
|---|---|
| Enter | 打开选中项 |
| Backspace | 返回上一级 |
| F2 | 重命名 |
| Delete | 删除 |
| F5 | 刷新 |
| Ctrl+A | 全选当前列表 |
| Ctrl+C | 复制 |
| Ctrl+X | 剪切 |
| Ctrl+V | 粘贴 |
| Alt+← | 后退 |
| Alt+→ | 前进 |
| Esc | 取消/关闭当前交互 |

## 写操作说明

写操作需要登录，并且当前账号必须对仓库有 push 权限。

每一次写操作都会直接在当前分支产生一个提交，包括：

- 新建文件
- 新建文件夹（Git 不支持空文件夹，因此需要同时创建第一个文件）
- 上传文件/文件夹
- 重命名
- 移动
- 复制
- 剪切/粘贴
- 删除

### 原子提交

项目使用 GitHub Git Data API，而不是 Contents API。流程大致为：

1. 读取当前分支头提交和根 tree
2. 构造本次改动涉及的 tree entry
3. 对新增/修改内容创建 blob
4. 创建新 tree
5. 创建新 commit
6. 更新分支 ref

因此一次操作即使涉及多个文件，也只会产生一个提交。若提交期间分支被别人更新，GitHub 会拒绝非快进更新，应用会提示刷新后重试。

## 项目结构

```text
RepoExplorer/
├─ main.go                     # Wails 启动入口，绑定 internal/app.App
├─ go.mod
├─ go.sum
├─ wails.json
├─ README.md
├─ frontend/
│  ├─ dist/                    # 前端源码：HTML/CSS/JS，无构建步骤
│  └─ wailsjs/                 # Wails 自动生成绑定
├─ internal/
│  ├─ app/                     # 应用后端主体
│  │  ├─ app.go                # App 状态与启动
│  │  ├─ auth.go               # GitHub OAuth / Token 登录
│  │  ├─ provider.go           # 通用 Git Provider 适配层
│  │  ├─ github_provider.go    # GitHub Provider 路由
│  │  ├─ github.go             # GitHub 仓库读取
│  │  ├─ gitdata.go            # GitHub 写操作：blob/tree/commit/ref
│  │  ├─ repos.go              # 我的仓库列表
│  │  ├─ download.go           # 下载与进度事件
│  │  ├─ mutate.go             # 上传、新建、移动、复制、删除等写操作
│  │  ├─ open.go               # 下载临时文件并用系统默认程序打开
│  │  ├─ httpclient.go         # HTTP 客户端与代理
│  │  ├─ proxy_*.go            # 平台代理适配
│  │  └─ systemopen_*.go       # 平台打开文件适配
│  ├─ githubutil/              # GitHub 地址解析与 ref/path 编码
│  └─ repopath/                # 仓库路径清理与安全拼接
└─ build/                      # 构建产物目录
```

## Provider 架构

Wails 暴露的 `App` 方法不直接依赖 GitHub API，而是通过 `gitProvider` 接口转发：

```go
type gitProvider interface {
    ID() string
    CanHandle(repo string) bool
    FetchRepoTree(...)
    DownloadFiles(...)
    OpenFile(...)
    ReadFile(...)
    SaveFile(...)
    DeletePaths(...)
    MovePaths(...)
    CopyPaths(...)
    UploadFiles(...)
}
```

当前实现：

```text
internal/app/github_provider.go
```

后续扩展其它服务时，可以新增：

```text
internal/app/gitlab_provider.go
internal/app/gitee_provider.go
internal/app/generic_git_provider.go
```

## 认证与凭据

凭据保存在本机：

```text
%APPDATA%/RepoExplorer/auth.json
```

支持：

- GitHub OAuth Device Flow
- 手动 Personal Access Token
- 清除本地凭据

如果 GitHub 返回 401，应用会清除失效登录状态并提示重新登录。

## 错误提示

错误提示不会自动消失，需要手动点击关闭。错误文本可选中复制，便于排查和反馈。

常见错误：

- 401：Token 无效或已过期
- 403：权限不足或 API 限流
- 404：仓库/分支不存在，或私有仓库未授权
- 409：分支状态冲突，刷新仓库后重试
- 502/503/504：远端服务临时异常，稍后重试

## 注意事项

- 当前写操作会直接提交到当前分支，不会创建 Pull Request
- 删除操作会真实删除文件，但可通过 Git 历史找回
- Git 不支持空文件夹
- 双击打开文件时会下载到临时目录；本地修改不会自动写回仓库
- 上传文件夹会保留最外层文件夹名

## License

未指定。
