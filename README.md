# mini_coder

参考 [Coder](https://github.com/coder/coder) 的独立学习项目，逐步实现开发工作区管理的核心功能：用户与权限、工作区生命周期、资源调度和 Web 界面，同时实践 Go 后端开发、Conventional Commits、CI/CD 与 semantic-release。前端优先采用 Vue。

## 当前状态

已实现 Chi HTTP 服务、统一失败响应、健康检查、优雅退出、PostgreSQL 持久化、首次管理员初始化、密码登录和 Session + Cookie 会话，以及测试和发布打包脚本、GitHub Actions 配置。用户管理、完整 RBAC、Docker 资源管理、前端和 semantic-release 尚未实现。

## 快速开始

Go 版本以 [go.mod](go.mod) 为准；另需 Git、Make、Bash、C 编译器，以及 Docker Engine 与 Compose v2（或可连接的 PostgreSQL 18.6）。

```bash
cp .env.example .env
set -a
. ./.env
set +a
make db-up
make migrate-up
make run
```

另开终端：

```bash
curl -i http://127.0.0.1:8080/healthz
```

响应为 HTTP 200 和 `{"status":"ok"}`。健康检查只报告进程存活。服务启动要求数据库可连接，迁移需预先显式执行。首次管理员初始化与登录操作见 [认证 API](docs/auth-api.md)。

首次创建测试数据库，再运行完整检查：

```bash
docker compose exec postgres createdb -U minicoder minicoder_test
make check
```

`make check` 包含生成代码一致性检查和真实数据库集成测试。连接配置、迁移回滚及发布包使用见 [本地开发](docs/development.md)。

## 文档

| 入口 | 内容 |
| --- | --- |
| [文档导航](docs/README.md) | 文档职责与阅读顺序 |
| [项目目标与路线](docs/project.md) | 核心功能范围、阶段目标与验收方向 |
| [架构草案](docs/architecture.md) | 当前结构、目标职责与 Coder 参考入口 |
| [本地开发](docs/development.md) | 环境、启动、配置、检查与测试 |
| [协作与发布](docs/workflow.md) | PR、Conventional Commits、CI 与发布 |
| [架构决策记录](docs/adr/README.md) | 决策索引、状态与模板 |
| [AI 协作入口](AGENTS.md) | AI 执行任务时需要遵循的简短约定 |
