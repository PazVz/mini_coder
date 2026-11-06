# mini_coder

参考 [Coder](https://github.com/coder/coder) 核心功能的独立学习项目，学习顺序是 Go 后端、GitHub Actions / CI/CD、前端。当前版本提供可运行的 Go HTTP 服务，以及通过 PR 检查、合并、发布的基础流程。

## 本地开发

需要 Go 1.27.2 或更新版本、Git、Make 和 Bash。CI 使用 `go.mod` 中的 Go 版本；项目目前只依赖标准库，因此没有 `go.sum`。`make test` 使用 race detector，在 Linux 上需要 GCC 等 C 编译器。

```bash
make run
```

另开终端访问：

```bash
curl -i http://127.0.0.1:8080/healthz
```

返回 HTTP 200、`Content-Type: application/json` 和：

```json
{"status":"ok"}
```

`/healthz` 用于检查 HTTP 服务是否存活，目前没有数据库等依赖检查。它支持 GET / HEAD，其他方法返回 405；不存在的路径返回 404。

默认监听 `127.0.0.1:8080`，可以通过环境变量或命令行参数修改；命令行参数优先：

```bash
MINICODER_ADDR=127.0.0.1:9090 make run
go run ./cmd/server -addr 127.0.0.1:9091
```

日志写入标准错误。服务配置了请求读写超时；收到 Ctrl+C / SIGTERM 时停止接受新连接，最多等待 5 秒完成现有请求，然后退出。

## 常用命令

| 命令 | 作用 |
| --- | --- |
| `make` | 查看常用命令 |
| `make fmt` | 格式化 Go 代码 |
| `make fmt-check` | 检查格式，不修改文件 |
| `make vet` | 运行 Go 静态分析 |
| `make test` | 运行测试、race detector 和覆盖率统计 |
| `make build` | 构建 `bin/mini-coder` |
| `make check` | 运行格式检查、静态分析、测试和构建，与 CI 一致 |
| `make dist VERSION=v0.1.0` | 在本地生成 Linux amd64 / arm64 发布包，不上传 |

运行编译后的程序：

```bash
make build
./bin/mini-coder -version
./bin/mini-coder -addr 127.0.0.1:8080
```

普通构建的版本是 `dev`；发布包会注入对应的版本标签。

## 代码结构

```text
cmd/server/                  进程入口、配置、信号处理和优雅退出
internal/httpapi/            HTTP 路由和接口测试
scripts/build-release.sh     交叉编译、打包和 SHA-256 校验和
.github/scripts/             PR 标题校验脚本
.github/workflows/           CI、PR 标题检查、版本发布
```

先阅读 `internal/httpapi/router.go` 与测试，再阅读 `cmd/server/main.go`。后续在 `internal/` 中逐步加入工作区业务和持久化逻辑。

## PR 开发流程

所有文件修改都通过开发分支提交 PR，合并方式使用 Squash。开始一个后续任务时：

```bash
git switch main
git pull --ff-only
git switch -c feat/workspace-api
```

完成修改后运行 `make check`，检查差异，再提交和推送开发分支。在 GitHub 创建以 `main` 为目标的 PR。

提交信息和 PR 标题使用 [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/)：

```text
chore: bootstrap Go service and CI
feat(workspace): add workspace creation
fix(api): handle invalid workspace IDs
feat(api)!: change workspace response format
```

标题检查接受 `feat`、`fix`、`docs`、`style`、`refactor`、`perf`、`test`、`build`、`ci`、`chore`、`revert`。scope 可省略；使用时由小写字母、数字、点、下划线、斜杠、连字符组成，并以字母或数字开头。描述不能为空，标题不能有首尾空白或换行。

可选的本地标题检查需要 Node.js，仅开发 Go 服务不需要 Node.js：

```bash
node .github/scripts/check-pr-title.cjs 'chore: bootstrap Go service and CI'
```

## GitHub Actions

| Workflow | 触发条件 | 检查 / 结果 |
| --- | --- | --- |
| `CI` | 发往 `main` 的 PR、`main` 推送、手动触发 | `Go CI` 执行 `make check` |
| `PR title` | PR 创建、改标题、更新代码、重新打开、转为待评审 | `PR Title` 校验标题格式 |
| `Release` | 推送 `v*` 标签 | 验证代码、打包并创建 GitHub Release |

在 GitHub 的 `Settings → Rules → Rulesets` 中保护 `main`：要求 PR、禁止强推和删除，独立开发时 required approvals 设为 0，Bypass list 留空。首次 PR 的 Actions 运行后，将 **Go CI** 和 **PR Title** 设为必需检查，再合并 PR。创建 workflow 文件本身不会自动配置这些仓库规则。

CI 与标题检查使用只读仓库权限；只有发布任务声明 `contents: write`。Actions 使用固定 commit SHA，并在注释中标注版本。Go 版本通过 `go.mod` 统一配置，后续升级时通过 PR 修改。

## 发布

当前交付目标是 GitHub Release 中的可下载程序。版本号由你选择，workflow 自动验证与打包；自动计算版本号的 semantic-release 和部署到服务器可以分别放到后续 PR 学习。

先将代码通过 PR 合并到 `main`，确认 CI 通过，再在本地执行：

```bash
git switch main
git pull --ff-only
git tag -a v0.1.0 -m 'Release v0.1.0'
git push origin v0.1.0
```

发布工作流要求标签对应的提交已进入远程 `main`，且版本格式为 `vMAJOR.MINOR.PATCH`（例如 `v0.1.0`，暂不支持预发布后缀）。它会重新执行 `make check`，生成两个 Linux 平台的 tar.gz 包与 `checksums.txt`，然后自动生成 Release notes 并上传附件。使用 GitHub 提供的 `GITHUB_TOKEN`，不需要额外 PAT。

可以先通过 `make dist VERSION=v0.1.0` 检查本地发布包。下载发布包后，在同一目录使用 `sha256sum --check checksums.txt --ignore-missing` 验证已下载的文件，然后解压并运行 `mini-coder`。一次版本发布使用一个新标签；不要移动已发布的标签。

## 后续学习顺序

1. Go API 与错误处理，加入 PostgreSQL、数据库迁移和集成测试。
2. 工作区创建、启动、停止、删除，以及 Docker 容器生命周期管理。
3. 模板、任务状态、日志和终端连接，再接入 React 或 Vue 界面。
