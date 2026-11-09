# ADR-0008：密码认证、持久化会话与统一 HTTP 失败响应

- 日期：2026-11-09
- 状态：已采纳，已实现；用户管理、角色授权及哈希限流范围由 [ADR-0009](0009-user-management-and-authorization.md) 扩展

## 背景

用户持久化已实现，需要完成首次管理员初始化和浏览器登录闭环。ADR-0003 与 ADR-0004 已选择 Chi 和服务端 Session + Cookie。新增接口需要一致的失败响应，供前端处理、字段提示及日志排查使用。

## 决定

### HTTP 契约

整个服务使用一个 Chi 根路由，包括 `/healthz` 的 GET / HEAD。认证接口放在 `/api/v1/auth`。业务层返回业务错误；HTTP 层集中映射状态码和 JSON，不将数据库模型作为响应契约。

失败响应固定包含 `code`、`message`、`request_id`；字段验证失败附带 `validations` 数组，每项包含 `field`、`code`、`detail`。客户端依赖稳定的错误码，文字允许调整。成功响应直接返回资源或接口结果。用户 bigint ID 编码为十进制字符串，避免 JavaScript 精度丢失。

参考 Coder 提供公共响应函数和字段验证列表的做法。实际阅读版本为提交 [`99fb0da86498cf464a5b4593e377e88415368ce2`](https://github.com/coder/coder/blob/99fb0da86498cf464a5b4593e377e88415368ce2/coderd/httpapi/httpapi.go)，路径 `coderd/httpapi/httpapi.go`。本项目增加机器错误码与请求 ID，内部错误详情仅写服务端日志。

请求 ID 由服务端生成，返回 `X-Request-ID`，不采用客户端传入的值。HTTP 层的 404、405、CSRF 拒绝、限流和响应尚未开始时的 panic 都使用该契约。405 返回 `Allow`。已经开始响应后发生 panic 时中止连接，无法替换为 JSON；Go HTTP 服务器在进入路由之前拒绝的协议错误也不属于本契约。

### 管理员初始化与角色

部署者配置至少 32 字节的随机 `MINICODER_SETUP_TOKEN`，通过 `X-Setup-Token` 调用初始化接口。未配置时禁止初始化写操作。成功创建管理员后不自动登录，部署者可删除配置并重启服务。

`auth_setup` 保存独立的单例初始化状态。初始化事务锁定该行，创建用户、赋予管理员角色并完成标记；失败整体回滚。删除用户不会重新开放初始化。已有用户保留为 member；初始化可使用一个未占用的用户名创建管理员。

`user_roles` 保存最小用户到角色关系，当前每个用户一个角色，仅允许 admin / member。创建用户默认赋予 member。完整 RBAC 权限矩阵、角色管理和用户管理 API 后续实现，当前角色只表示身份，不隐含跨用户资源访问权限。

### 密码与会话

使用 Go `x/crypto/argon2` 的 Argon2id：内存 19 MiB、迭代 2、并行度 1、16 字节随机盐、32 字节结果，以包含版本和参数的 PHC 格式存储。当前仅接受这一组参数，避免损坏的数据触发无界内存消耗；参数升级时一起设计重新哈希流程。密码至少 12 个 Unicode 字符、最多 1024 字节，不裁剪、不隐式转换，不限制字符种类。用户名沿用 ADR-0007。

登录使用常量时间结果比较；未知用户执行同等参数的虚拟密码校验。用户名不存在、密码不匹配、账号停用返回相同的登录失败响应。此前数据层允许写入任意非空哈希；认证只支持上述 Argon2id 格式，旧的测试占位哈希不能登录。

每次成功登录生成新的 32 字节随机令牌，URL-safe Base64 编码后通过 Cookie 返回。PostgreSQL `sessions` 只保存 SHA-256 摘要、用户 ID、创建时间和绝对过期时间。有效期固定 24 小时，不滑动续期。每次认证读取当前用户状态与角色；会话过期即拒绝。服务每小时删除过期行，清理失败写日志，不影响过期判定。

登录创建会话前锁定并重新检查用户行。账号状态从 active 变为 disabled 时，数据库触发器在同一事务删除该用户全部会话，因此管理 SQL 也能执行撤销，且不会遗漏并发登录。重新启用不会恢复旧会话。退出只删除当前会话，并清除 Cookie，重复调用返回 204。删除用户通过外键级联删除会话和角色关系。

### Cookie、CSRF 与请求限制

Cookie 名为 `minicoder_session`，设置 HttpOnly、SameSite=Lax、Path=/，不设置 Domain。Secure 默认启用，本地 HTTP 可明确配置关闭。浏览器 UI 与 API 按同源部署。

采用 Go 标准库 `http.CrossOriginProtection`，通过 Fetch Metadata 或 Origin 检查拒绝不安全的跨源请求，包括登录、初始化和退出；同站但跨源请求也拒绝。不配置可信跨源白名单，不开放 CORS。缺少这两类头部的非浏览器请求允许通过。GET / HEAD / OPTIONS 不执行写操作。

初始化和登录只接受单个 JSON 对象，拒绝未知字段，限制请求体为 16 KiB。两者共享进程内限制：每个连接来源 IP 每分钟 10 次、全进程每分钟 60 次，最多 4 个同时执行。计数包含失败请求，窗口按首个请求开始，不信任转发 IP 头；限流响应附带 `Retry-After`。这些限制服务于当前单进程部署，多副本或可信代理策略另行设计。

## 备选方案

- RFC 9457 Problem Details 提供标准化错误文档。本项目采用 Coder 风格的简短结构，加稳定错误码和字段错误即可满足当前客户端需求。
- bcrypt 的密码长度限制不适合这里的长密码规则；采用 OWASP 推荐的 Argon2id。
- JWT、Redis 或专用会话库增加当前单体不需要的状态管理方式，沿用 PostgreSQL 和现有事务即可。
- 仅靠 SameSite 无法覆盖全部 CSRF 场景；标准库跨源检查适合同源 UI，无需增加客户端 CSRF token 状态。

## 后果

增加 Chi 和 x/crypto 固定版本依赖，以及一版可回滚迁移。回滚认证迁移会删除全部会话、角色和初始化状态，保留用户；重新升级会重新开放初始化，应仅用于可丢弃的环境。

已有会话可跨进程重启继续使用。账号停用的撤销规则位于数据库，需要随未来密码修改、用户管理和长连接功能继续扩展。当前未实现公开注册、密码找回、MFA、完整 RBAC、终端会话关闭或多副本共享限流。

## 关联

- [认证 API](../auth-api.md)
- [OWASP 密码存储建议](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html)
- [Go Argon2 包](https://pkg.go.dev/golang.org/x/crypto/argon2)
- [Go CrossOriginProtection](https://pkg.go.dev/net/http#CrossOriginProtection)
