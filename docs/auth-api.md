# 认证 API

当前实现首次管理员初始化、密码登录、当前用户查询和退出。浏览器通过服务端 Session + Cookie 认证。实现选择及限制见 [ADR-0008](adr/0008-authentication-and-http-errors.md)。

## 接口

| 方法与路径 | 请求 | 成功结果 |
| --- | --- | --- |
| `GET /api/v1/auth/setup` | 无需登录 | 200，`{"needs_setup":true}` 或 false |
| `POST /api/v1/auth/setup` | `X-Setup-Token` 和用户名、密码 JSON | 201，管理员用户；不自动登录 |
| `POST /api/v1/auth/login` | 用户名、密码 JSON | 200，用户；通过 Set-Cookie 创建会话 |
| `GET /api/v1/auth/me` | 会话 Cookie | 200，当前用户 |
| `POST /api/v1/auth/logout` | 会话 Cookie，可为空 | 204，无响应体；撤销当前会话并清除 Cookie |

初始化和登录请求结构：

```json
{"username":"admin","password":"choose a long unique password"}
```

用户名为 3～32 个 ASCII 字符，首字符是小写字母或数字，其余允许小写字母、数字、下划线和连字符；不自动转小写或去除空白。密码至少 12 个 Unicode 字符、最多 1024 字节。请求使用 `Content-Type: application/json`，允许 charset 参数，限制 16 KiB，拒绝未知字段及多个 JSON 值。初始化与登录执行相同的字段验证；格式不合法返回 400，格式合法但凭据不匹配返回 401。

用户响应示例：

```json
{
  "id": "1",
  "username": "admin",
  "status": "active",
  "role": "admin",
  "created_at": "2026-11-09T00:00:00Z",
  "updated_at": "2026-11-09T00:00:00Z"
}
```

ID 使用字符串，时间为 RFC 3339。响应不包含密码哈希、会话令牌或数据库内部字段。所有认证响应都设置 `Cache-Control: no-store`。

## 失败响应

由路由处理的失败响应使用正确的 HTTP 状态码和 `Content-Type: application/json`：

```json
{
  "code": "validation_failed",
  "message": "请求参数不合法",
  "validations": [
    {"field":"password","code":"invalid_length","detail":"密码至少 12 个字符，最多 1024 字节"}
  ],
  "request_id": "server-generated-request-id"
}
```

`code`、`message`、`request_id` 必填；只有字段错误包含 `validations`。前端按 `code` 判断，不解析提示文字。字段名对应 JSON 字段；字段错误码当前为 `invalid_format`、`invalid_length`。服务端日志与 `X-Request-ID` 响应头使用相同请求 ID。内部错误详情只写日志。

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | `invalid_json` | JSON 格式、字段类型、未知字段或文档数量不合法 |
| 400 | `validation_failed` | 用户名或密码规则不满足 |
| 401 | `invalid_credentials` | 用户名不存在、密码错误或账号停用，提示相同 |
| 401 | `unauthenticated` | Cookie 缺失、格式错误、未知、已撤销或已过期 |
| 403 | `setup_disabled` | 未配置初始化令牌 |
| 403 | `forbidden` | 初始化令牌不匹配 |
| 403 | `csrf_failed` | 不安全的跨源浏览器请求 |
| 404 | `not_found` | 路由或资源不存在 |
| 405 | `method_not_allowed` | 方法不支持，附带 `Allow` |
| 409 | `already_initialized` | 提供有效初始化令牌，但已初始化完成 |
| 409 | `username_taken` | 初始化用户名被已有用户占用 |
| 413 | `request_too_large` | 请求体超过 16 KiB |
| 415 | `unsupported_media_type` | 初始化或登录请求未使用 JSON |
| 429 | `rate_limited` | 请求频率或并发达到上限，附带 `Retry-After` 秒数 |
| 500 | `internal_error` | 数据库或其他内部错误，使用请求 ID 排查日志 |

中间件按顺序处理请求，可能在字段验证之前拒绝跨源或限流请求。HTTP 协议解析阶段的错误、已开始响应后的连接中止，不保证 JSON 响应。

## 本地操作

先按 [本地开发](development.md) 启动数据库并完成迁移。在启动服务的终端中配置初始化令牌；每次启动时生成新值只影响尚未完成的初始化，不影响已保存会话：

```bash
set -a
. ./.env
set +a
export MINICODER_COOKIE_SECURE=false
export MINICODER_SETUP_TOKEN="$(openssl rand -hex 32)"
make migrate-up
make run
```

将该令牌安全地设置到另一个终端的同名环境变量，再执行下列请求。初始化完成后，移除服务的 `MINICODER_SETUP_TOKEN` 并重启；数据库标记会继续阻止重复初始化。

```bash
curl -i http://127.0.0.1:8080/api/v1/auth/setup

curl -i -X POST http://127.0.0.1:8080/api/v1/auth/setup \
  -H 'Content-Type: application/json' \
  -H "X-Setup-Token: $MINICODER_SETUP_TOKEN" \
  --data '{"username":"admin","password":"choose a long unique password"}'

# Cookie 文件包含登录凭据，使用临时文件并限制权限。
cookie_jar="$(mktemp)"
chmod 600 "$cookie_jar"
curl -i -c "$cookie_jar" -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  --data '{"username":"admin","password":"choose a long unique password"}'
curl -i -b "$cookie_jar" http://127.0.0.1:8080/api/v1/auth/me
curl -i -b "$cookie_jar" -c "$cookie_jar" -X POST http://127.0.0.1:8080/api/v1/auth/logout
curl -i -b "$cookie_jar" http://127.0.0.1:8080/api/v1/auth/me
rm "$cookie_jar"
```

最后一次 me 请求返回 401。示例密码用于演示，部署时使用自己的密码。

## 会话与部署

- Cookie 为 `minicoder_session`，HttpOnly、SameSite=Lax、Path=/、无 Domain；Secure 默认 true，本地 HTTP 才设置 false。
- 会话绝对有效期 24 小时；重复登录创建独立会话，退出只撤销当前会话。过期判定实时执行，服务每小时清理过期数据库行。
- 停用账号会撤销全部会话，重新启用不会恢复旧会话。每次认证读取当前状态与角色。用户管理 API 尚未提供。
- 浏览器 UI 与 API 同源部署；Go 跨源保护拒绝不安全的跨源请求，包括同站不同源。无 Origin / Sec-Fetch-Site 的 CLI 请求可用；不开放跨源 CORS。反向代理保留原始 Host 并使用 HTTPS。
- 初始化和登录共享进程内限流：每个连接来源 IP 每分钟 10 次，全进程每分钟 60 次，最多 4 个同时执行。包括失败请求；不信任 X-Forwarded-For 等转发头。反向代理后的客户端会共享代理 IP 的配额；多副本共享限流尚未实现。
- 已有用户升级后为 member；使用一个未占用的用户名初始化管理员。旧的测试占位密码哈希不能登录；认证仅支持当前参数的 Argon2id。
- 当前没有公开注册、找回密码、修改密码或完整 RBAC API。
