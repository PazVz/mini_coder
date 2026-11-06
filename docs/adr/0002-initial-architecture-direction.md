# ADR-0002：Go 单体、Docker 与 Vue 的初期架构方向

- 日期：2026-11-06
- 状态：提议

## 背景

项目希望学习 Coder 的核心功能，已有 Go 服务，前端优先 Vue。初期需要跑通权限与基于容器的工作区管理流程。

## 决定（待采纳）

参考 [Coder 的职责划分](https://coder.com/docs/install/plan/architecture)，采用 Go 模块化单体，资源执行先接 Docker API，后台任务随需求加入同一服务。前端优先 Vue + TypeScript，考虑 Vite。持久化优先评估 PostgreSQL + pgx + sqlc，迁移工具建议 Goose；数据层工具随首个相关功能 PR 确定。

已单独确定的决定包括 [Chi 路由](0003-http-routing-with-chi.md)、[Session + Cookie](0004-server-side-browser-sessions.md)、[RBAC 资源授权](0005-rbac-resource-authorization.md) 和 [先终端、后 IDE](0006-terminal-before-ide.md)。接入设计见 [工作区访问设计](../workspace-access.md)。

版本在实际引入时核实官方稳定发布并固定，不在这份方向性记录中预先指定版本号。

## 备选方案

- 从一开始引入独立 provisioner、Terraform 和多服务部署：增加初期运行组件和排查成本，可在出现多执行环境需求后评估。
- 采用 React 前端：可行，但 Vue 更符合当前学习偏好。
- 先用 SQLite 持久化：运行简单；PostgreSQL 更贴合后续事务、并发任务和数据库开发的学习目标，仍需在数据层实现时确认。

## 后果

部署组件较少，可以先验证业务闭环，同时保留清晰职责。单 Docker 环境阶段主要学习任务调度和资源限制，多主机调度另行设计。出现独立扩缩容、多个资源后端或部署隔离需求时，重新评估进程边界。

对应目标与职责见 [架构草案](../architecture.md)。
