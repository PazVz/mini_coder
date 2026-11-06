# mini_coder

参考 [Coder](https://github.com/coder/coder) 的独立学习项目，逐步实现开发工作区管理的核心功能：用户与权限、工作区生命周期、资源调度和 Web 界面，同时实践 Go 后端开发、Conventional Commits、CI/CD 与 semantic-release。前端优先采用 Vue。

## 当前状态

已实现 Go HTTP 服务、健康检查、优雅退出、测试、本地检查与发布打包脚本，以及 GitHub Actions 配置。用户权限、数据库、Docker 资源管理、前端和 semantic-release 尚未实现。

## 快速开始

Go 版本以 [go.mod](go.mod) 为准；另需 Git、Make、Bash，以及运行 race detector 所需的 C 编译器。

```bash
make run
```

另开终端：

```bash
curl -i http://127.0.0.1:8080/healthz
```

响应为 HTTP 200 和 `{"status":"ok"}`。代码变更提交前运行 `make check`。

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
