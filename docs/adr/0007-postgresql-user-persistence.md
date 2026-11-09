# ADR-0007：PostgreSQL 与最小用户持久化

- 日期：2026-11-07
- 状态：已采纳

## 背景

当前 HTTP 服务需要为登录会话、权限和工作区建立持久化基础。本次先交付数据库接入与用户读写，后续继续实现已采纳的 Session + Cookie 和 RBAC。

## 决定

采用 PostgreSQL、pgx、sqlc 和 Goose。PostgreSQL 保存关系数据和执行约束；pgxpool 管理连接；手写 SQL 经 sqlc 生成 Go 类型和查询方法；Goose 管理版本化 SQL 迁移。版本固定在 Compose、go.mod 和 Makefile 中，核实日期及官方来源见 [本地开发](../development.md)。

迁移 SQL 和查询 SQL 分别放在 `internal/database/migrations/` 与 `queries/`，生成代码提交到 `dbgen/`。通过 `make generate-check` 检查生成结果是否与 SQL 一致。当前直接使用生成查询，需要业务规则时再增加业务包。

迁移由独立的 `mini-coder-migrate` 命令执行，SQL 嵌入二进制，使用 PostgreSQL advisory lock 串行化迁移。服务启动时要求数据库配置并在限定时间内验证连接；服务启动不执行迁移。连接池在 HTTP 服务退出后关闭。`/healthz` 继续只报告进程存活。

用户模型采用以下约束：

- `id`：数据库生成的 bigint identity 主键，供后续会话和工作区引用。
- `username`：唯一、3～32 个 ASCII 字符；首字符为小写字母或数字，其余允许小写字母、数字、下划线和连字符。存储层拒绝大写及空白，不做隐式转换。
- `password_hash`：只接收调用方生成的密码哈希，要求非空。当前存储层不生成或验证密码哈希；密码算法随认证功能确定。
- `status`：`active` 或 `disabled`，创建时默认为 `active`。
- `created_at`、`updated_at`：非空 timestamptz，创建时由数据库赋值；未来更新查询必须同步设置 `updated_at`，当前没有更新接口。

本次提供创建用户、按 ID 查询和按用户名查询，事务通过 sqlc 的 `WithTx` 组合。数据库错误保留 pgx 的无记录错误和 PostgreSQL SQLSTATE，供后续业务层映射。持久化模型不作为 HTTP 响应契约，密码哈希字段额外标记 `json:"-"`。

本次不引入用户管理 API、密码认证、会话表或角色关系表；它们分别在登录与授权功能中实现。

## 备选方案

- SQLite：运行依赖更少，但 PostgreSQL 更符合后续事务、并发任务和数据库开发的学习目标。
- ORM：减少部分 SQL 编写，但本项目希望直接学习表结构、约束和查询，采用 sqlc 保留 SQL 控制权。
- 手写全部 pgx 扫描代码：初期可行，但 sqlc 能统一维护查询参数、返回类型和扫描顺序。
- 服务启动时自动迁移：本地启动方便，但会把结构修改和进程启动绑定；独立命令更便于观察、验证和控制迁移。

## 后果

开发和完整验证需要 PostgreSQL。提供本地 Compose 配置，并为 CI 与发布验证配置相同版本的数据库服务。集成测试要求显式提供测试数据库连接，在随机独立 schema 内执行迁移与数据验证，测试完成后清理自身 schema。

数据库约束作为并发写入的最终保证；未来 API 仍需在输入边界验证并提供可理解的错误。首次迁移的回滚会删除用户表及其中的数据，适用于可丢弃的开发或测试数据。

发布包同时包含服务和迁移二进制。数据库部署、备份与升级仍需独立管理。
