# 本地开发

## 环境与版本

Go 版本由 [go.mod](../go.mod) 声明，目前为 `1.27.2`，CI 从该文件读取；本地优先对齐。另需 Git、Make、Bash 和 C 编译器，Linux 下可使用 GCC，编译器用于 `make test` 的 race detector。

Node.js 用于 PR 标题检查；前端工程尚未创建。当前 Go 服务无需 Docker，资源管理功能接入时再补充 Docker Engine 的版本和连接要求。

新增或升级依赖时查阅官方发布记录，优先选择当前稳定且相互兼容的版本，记录核实日期及来源。Go 对齐 `go.mod`；Node 优先使用受支持的 LTS，前端建立时增加版本文件；前端依赖提交锁文件。已有版本通过独立、可验证的变更升级，文档中的版本说明不代替工具链配置。

## 启动与配置

```bash
make run
```

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

以上是两种独立的启动方式。日志写入标准错误；服务配置请求读写超时。收到 Ctrl+C / SIGTERM 后停止接受新连接，最多等待 5 秒完成现有请求。

## 常用命令

从仓库根目录运行：

| 命令 | 作用 |
| --- | --- |
| `make` | 查看常用命令 |
| `make run` | 启动服务 |
| `make fmt` | 格式化 Go 代码 |
| `make fmt-check` | 检查格式，不修改文件 |
| `make vet` | Go 静态分析 |
| `make test` | 测试、race detector 和覆盖率统计 |
| `make build` | 构建 `bin/mini-coder` |
| `make check` | 格式检查、静态分析、测试和构建，与 CI 的 Go 检查一致 |
| `make dist VERSION=v0.1.0` | 生成 Linux amd64 / arm64 发布包，不上传 |

```bash
make build
./bin/mini-coder -version
./bin/mini-coder -addr 127.0.0.1:8080
```

普通构建版本为 `dev`，发布包注入指定版本。当前仅依赖 Go 标准库，所以没有 `go.sum`。

## 实现与测试原则

Go 优先使用标准库和清晰的包职责，错误附带有用上下文，取消和超时使用 `context`。前端接入后遵循 Vue 的官方惯例，按页面与职责组织组件，优先局部状态，跨页面共享需求出现时再引入相应工具。

测试围绕可观察行为编写。当前用 `testing` 与 `httptest`；后续重点覆盖权限允许与拒绝、数据约束、状态转换及 Docker 执行失败等路径。数据库和容器行为用相应集成测试验证，前端覆盖关键交互，核心闭环补充少量端到端测试。覆盖率用于发现遗漏，不设机械数字目标。

代码变更提交前运行 `make check`，针对新行为补充必要验证。纯文档变更检查链接、示例命令、当前实现和计划之间的一致性；无需为文字变化新增测试。PR 中记录实际运行结果，流程见 [协作与发布](workflow.md)。
