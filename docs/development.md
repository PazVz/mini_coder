# 本地开发

## 环境与版本

Go 版本由 [go.mod](../go.mod) 声明，目前为 `1.27.2`，CI 从该文件读取；本地优先对齐。另需 Git、Make、Bash 和 C 编译器，Linux 下可使用 GCC，编译器用于 `make test` 的 race detector。

Node.js 用于 PR 标题检查；前端工程尚未创建。本地数据库使用 Docker Engine 与 Compose v2，先通过 `docker version` 和 `docker compose version` 确认可用；也可连接独立部署的 PostgreSQL。

新增或升级依赖时查阅官方发布记录，优先选择当前稳定且相互兼容的版本，记录核实日期及来源。Go 对齐 `go.mod`；Node 优先使用受支持的 LTS，前端建立时增加版本文件；前端依赖提交锁文件。已有版本通过独立、可验证的变更升级，文档中的版本说明不代替工具链配置。

本次数据层版本于 2026-11-07 核实：

| 组件 | 固定版本与位置 | 官方来源 |
| --- | --- | --- |
| PostgreSQL | `18.6-bookworm`，Compose 与 workflow | [支持版本](https://www.postgresql.org/support/versioning/)、[官方镜像](https://hub.docker.com/_/postgres) |
| pgx | `v5.11.0`，go.mod / go.sum | [发布记录](https://github.com/jackc/pgx/releases/tag/v5.11.0) |
| Goose | `v3.29.0`，go.mod / go.sum | [发布记录](https://github.com/pressly/goose/releases/tag/v3.29.0) |
| sqlc | `v1.31.1`，Makefile | [发布记录](https://github.com/sqlc-dev/sqlc/releases/tag/v1.31.1) |

认证依赖于 2026-11-09 核实并固定：Chi `v5.5.0`（[官方发布](https://github.com/go-chi/chi/releases/tag/v5.5.0)）、Go x/crypto `v0.58.0`（[官方模块文档](https://pkg.go.dev/golang.org/x/crypto@v0.58.0)），具体版本见 go.mod / go.sum。

sqlc 通过固定版本的 `go run` 执行，无需全局安装。首次生成或完整检查会下载并编译工具依赖。

## 启动与配置

```bash
cp .env.example .env
set -a
. ./.env
set +a
make db-up
make migrate-up
make run
```

`.env` 被 Git 忽略，程序和 Makefile 不自动加载它；上面的 shell 命令将配置导出到当前终端。示例账号和密码仅用于本地开发，Compose 仅绑定 `127.0.0.1:5432`。连接外部数据库时修改 `MINICODER_DATABASE_URL`，按部署环境配置 TLS。

`MINICODER_DATABASE_URL` 必填，支持 pgx 接受的 PostgreSQL URL 或连接字符串。服务在启动时验证连接，最多等待 5 秒；失败则退出。连接成功不表示迁移已完成，启动前执行 `make migrate-up`。

认证配置如下；已有 `.env` 升级时也需补充本地 HTTP 的 Cookie 配置：

| 变量 | 默认与作用 |
| --- | --- |
| `MINICODER_SETUP_TOKEN` | 空值禁用初始化写接口；启用时至少 32 字节，建议 `openssl rand -hex 32` 生成 |
| `MINICODER_COOKIE_SECURE` | 默认 `true`；本地 HTTP 明确设置 `false`，HTTPS 保持 `true` |

初始化管理员、登录和退出示例见 [认证 API](auth-api.md)。

另开终端：

```bash
curl -i http://127.0.0.1:8080/healthz
```

健康检查返回 HTTP 200、`Content-Type: application/json` 和 `{"status":"ok"}`。`/healthz` 检查进程存活，支持 GET / HEAD；其他方法返回 405，不存在的路径返回 404。目前没有依赖健康检查。

默认监听 `127.0.0.1:8080`。可通过环境变量或参数修改，参数优先：

```bash
MINICODER_ADDR=127.0.0.1:9090 make run
go run ./cmd/server -addr 127.0.0.1:9091
```

以上是两种独立的启动方式，都需要数据库环境变量。日志写入标准错误；服务配置请求读写超时。收到 Ctrl+C / SIGTERM 后停止接受新连接，最多等待 5 秒完成现有请求，再关闭数据库连接池。

## 数据迁移与查询

```bash
make migrate-status
make migrate-up
```

迁移命令有 1 分钟超时，并以数据库 advisory lock 防止并发执行迁移。重复执行 `migrate-up` 不会再次应用已完成的版本。迁移 SQL 嵌入迁移程序，执行二进制时无需源码目录。

`make migrate-down` 回滚最近一次迁移；认证迁移回滚会删除会话、角色和初始化状态，保留用户，重新升级会重新开放初始化；继续回滚第一版会删除整个用户表及其数据，只在可丢弃的数据环境中使用。日常数据库升级通过新增迁移完成，已应用的迁移文件保持不变。

修改 `internal/database/queries/` 或新增迁移后执行 `make generate`，提交 SQL 和 `internal/database/dbgen/` 中的生成结果。`make generate-check` 检查生成文件是否同步。用户模型和用户名规则见 [ADR-0007](adr/0007-postgresql-user-persistence.md)，认证表结构及约定见 [ADR-0008](adr/0008-authentication-and-http-errors.md)。

`make db-down` 停止并移除本项目容器，保留数据库卷；再次执行 `make db-up` 会复用已有数据。初次创建卷后修改 Compose 的账号或密码不会修改已有数据库账号。

## 测试数据库

首次启动本地 PostgreSQL 后创建专用测试数据库：

```bash
docker compose exec postgres createdb -U minicoder minicoder_test
```

`.env.example` 已包含指向该数据库的 `MINICODER_TEST_DATABASE_URL`。如果数据库运行在其他位置，单独设置这个变量。

```bash
make test
make test-integration
make check
```

`make test` 无需数据库；`test-integration` 使用真实 PostgreSQL，要求配置测试连接，缺失配置或无法连接时失败。每个测试创建随机 schema，限定 `search_path`，并在完成后清理自身 schema；账号需要创建 schema 的权限。测试验证迁移升级、重复升级、回滚及重新升级、用户持久化、数据约束、初始化竞争、会话持久化与撤销、账号停用竞争，以及使用真实 Cookie 的 HTTP 登录闭环。不要指向生产数据库。

`make check` 包含单元测试与集成测试。CI 和发布 workflow 使用相同 PostgreSQL 镜像及专用测试库。

## 常用命令

从仓库根目录运行：

| 命令 | 作用 |
| --- | --- |
| `make` | 查看常用命令 |
| `make run` | 启动服务 |
| `make db-up` / `make db-down` | 启停本地数据库，保留卷 |
| `make migrate-up` / `make migrate-status` | 应用迁移 / 查看状态 |
| `make migrate-down` | 回滚最近一次迁移，可能删除数据 |
| `make generate` / `make generate-check` | 生成查询代码 / 检查生成结果 |
| `make fmt` | 格式化 Go 代码 |
| `make fmt-check` | 检查格式，不修改文件 |
| `make vet` | Go 静态分析 |
| `make test` | 单元及 HTTP 测试、race detector 和覆盖率统计 |
| `make test-integration` | 真实 PostgreSQL 集成测试与 race detector |
| `make build` | 构建服务与迁移二进制到 `bin/` |
| `make check` | 格式、生成结果、静态分析、单元与集成测试、构建 |
| `make dist VERSION=v0.1.0` | 生成 Linux amd64 / arm64 发布包，不上传 |

```bash
make build
./bin/mini-coder -version
./bin/mini-coder-migrate up
./bin/mini-coder -addr 127.0.0.1:8080
```

普通服务构建版本为 `dev`，发布包注入指定版本。发布包包含 `mini-coder` 和 `mini-coder-migrate`，执行迁移及服务前导出 `MINICODER_DATABASE_URL`。Go 依赖和校验和分别提交在 `go.mod` 与 `go.sum`。

## 实现与测试原则

Go 优先使用标准库和清晰的包职责，错误附带有用上下文，取消和超时使用 `context`。前端接入后遵循 Vue 的官方惯例，按页面与职责组织组件，优先局部状态，跨页面共享需求出现时再引入相应工具。

测试围绕可观察行为编写。当前使用 `testing`、`httptest` 和真实 PostgreSQL 集成测试；后续重点覆盖权限允许与拒绝、状态转换及 Docker 执行失败等路径。容器行为用相应集成测试验证，前端覆盖关键交互，核心闭环补充少量端到端测试。覆盖率用于发现遗漏，不设机械数字目标。

代码变更提交前运行 `make check`，针对新行为补充必要验证。纯文档变更检查链接、示例命令、当前实现和计划之间的一致性；无需为文字变化新增测试。PR 中记录实际运行结果，流程见 [协作与发布](workflow.md)。

### 代码可读性约定

遵循 [Effective Go](https://go.dev/doc/effective_go) 和 [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)。以下约定用于统一手写 Go 代码；生成代码通过生成器维护。

- **格式**：使用现有 `make fmt`、`make fmt-check` 和 `make vet`；缩进、空格、对齐交给 `gofmt`。按语义换行，不设固定行宽或函数行数上限。
- **命名**：使用 MixedCaps，缩写保持 `ID`、`HTTP`、`URL` 等惯用形式；业务变量和函数名表达对象与作用。短作用域保留 `ctx`、`err`、`tx`、`r`、`w` 等惯用名称，同一类型的接收器名称一致；HTTP 方法和状态码使用 `net/http` 常量。
- **布局**：业务结构体初始化写字段名，多字段声明及复杂初始化分行；多步骤逻辑不压成单行，独立函数间留空行。简单单字段类型、单步骤函数可以紧凑书写。
- **导入**：按标准库、第三方依赖、本项目包分组，组间空行；组内排序交给 `gofmt`。
- **注释**：代码注释沿用英文，导出声明有以声明名称开头的简短说明；非直观的事务、并发、安全和资源释放逻辑解释原因。跨 Go、SQL、触发器的关键约束标明实现位置，不逐行复述代码。
- **控制流**：错误优先处理并提前返回，保持正常路径清晰；错误附带操作上下文，需要识别原因时保留错误链。`context.Context` 放首参数；接口按调用方实际需求定义。
- **JSON**：HTTP 请求与响应字段显式声明标签，沿用现有 `snake_case` 契约；字段名、`omitempty` / `omitzero` / `string` / `-` 及 `null` 与空数组的差异按行为评审，不作为格式调整。语义见 [encoding/json](https://pkg.go.dev/encoding/json#Marshal)。
- **测试**：多场景测试使用可辨认的名称，失败信息给出必要输入、实际结果和预期；不同语义的断言分开。简单匿名测试表可按字段顺序初始化，每个场景独立一行，复杂场景展开。

具名初始化、导入三组和测试布局是本项目约定，`gofmt` / `go vet` 不会全面检查；交付前按以上规则检查本次差异，避免复制附近的旧写法。
