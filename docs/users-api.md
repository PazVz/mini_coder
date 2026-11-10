# 用户管理 API

管理员可以创建用户、分页查询用户列表、启停账号和调整内置角色。所有接口要求有效会话，普通用户返回 403；普通用户查询自己使用 [认证 API](auth-api.md) 的 `/api/v1/auth/me`。权限矩阵及并发规则见 [ADR-0009](adr/0009-user-management-and-authorization.md)。

## 接口

| 方法与路径 | 输入 | 成功结果 |
| --- | --- | --- |
| `POST /api/v1/users` | `username`、`email`、`password` JSON | 201，新用户，默认 active/member |
| `GET /api/v1/users` | 可选 `after_id`、`limit` 查询参数 | 200，用户分页结果 |
| `PATCH /api/v1/users/{id}/status` | `{"status":"active"}` 或 `{"status":"disabled"}` | 200，更新后的用户 |
| `PATCH /api/v1/users/{id}/role` | `{"role":"admin"}` 或 `{"role":"member"}` | 200，更新后的用户 |

用户字段与认证响应一致：`id`、`username`、`email`、`email_verified`、`status`、`role`、`created_at`、`updated_at`。ID 使用十进制字符串，时间使用 RFC 3339，不返回密码哈希或会话令牌。

写接口接受 `Content-Type: application/json`，请求体为单个对象，最多 16 KiB；拒绝未知字段。创建时指定 role、status 或邮箱验证状态会被拒绝。用户名、邮箱与密码规则沿用认证 API，用户名或邮箱冲突返回 409。邮箱必填且初始未验证；邮箱自助维护随后续验证流程一起开放。路径 ID 须为正的 int64 范围整数；格式错误返回 400，有效 ID 对应的用户不存在返回 404。

PATCH 仅修改路径指定的字段，不是通用 JSON Patch。字段缺失、null、空字符串或枚举值不合法均返回 400。相同值重复请求返回当前用户，保持更新时间不变；实际修改同步更新 `updated_at`。

## 分页

`GET /api/v1/users?after_id=0&limit=50` 按 ID 升序查询 ID 大于 after_id 的用户。after_id 默认为 0，须为非负的 int64 范围整数；limit 默认为 50，允许 1～100。未知、重复、空值或格式不合法的查询参数返回 400。

```json
{
  "users": [
    {
      "id": "1",
      "username": "admin",
      "email": "admin@example.com",
      "email_verified": false,
      "status": "active",
      "role": "admin",
      "created_at": "2026-11-09T00:00:00Z",
      "updated_at": "2026-11-09T00:00:00Z"
    }
  ],
  "next_after_id": "1"
}
```

有下一页时，将 next_after_id 作为下一次请求的 after_id；最后一页省略该字段。无结果返回 `{"users":[]}`。管理员可查看所有用户，包括停用账号；分页不提供跨请求的一致性快照。

## 状态、角色与会话

- 停用账号撤销它的全部会话，并阻止登录；重新启用后须重新登录。
- 角色调整保留会话，后续请求使用当前角色。管理员可以调整自己的状态或角色。
- 若停用或降级会移除最后一个启用中的管理员，返回 409 `last_admin`；并发请求也受该约束保护。
- 管理写事务取得数据库锁后重新检查操作者权限。等待期间被降级的操作者返回 403，被停用的操作者返回 401。
- 所有响应设置 `Cache-Control: no-store`，写请求执行全局跨源保护。创建用户与初始化、登录、修改密码共享频率和哈希并发限制，达到上限返回 429 和 `Retry-After`。

## 错误

失败响应沿用认证 API 的 `code`、`message`、`request_id` 和可选 `validations`。主要业务错误如下：

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | `validation_failed` | 凭据、ID、分页参数、状态或角色不合法 |
| 401 | `unauthenticated` | 缺少有效会话，或操作者已停用 |
| 403 | `forbidden` | 当前角色没有管理权限 |
| 404 | `not_found` | 目标用户不存在 |
| 409 | `username_taken` | 用户名已占用 |
| 409 | `email_taken` | 邮箱已占用，包括仅大小写不同 |
| 409 | `last_admin` | 操作会移除最后一个启用中的管理员 |

状态、角色、ID 和分页参数的字段错误码为 `invalid_value`；用户名、密码沿用既有字段错误码；邮箱格式使用 `invalid_format`。JSON、Content-Type、请求大小、CSRF、限流和内部错误沿用 [统一失败响应](auth-api.md#失败响应)。请求解析发生在业务授权之前，因此请求格式不合法时可能先返回 400 或 415。

## 本地示例

先完成数据库迁移、服务启动与首次管理员初始化。以下密码为演示值，替换为本地账号密码：

```bash
cookie_jar="$(mktemp)"
chmod 600 "$cookie_jar"
curl -i -c "$cookie_jar" -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  --data '{"username":"admin","password":"choose a long unique password"}'

curl -i -b "$cookie_jar" -X POST http://127.0.0.1:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  --data '{"username":"alice","email":"alice@example.com","password":"another long unique password"}'
curl -i -b "$cookie_jar" 'http://127.0.0.1:8080/api/v1/users?limit=50'

# 将 2 替换为新创建用户的响应 ID。
user_id=2
curl -i -b "$cookie_jar" -X PATCH "http://127.0.0.1:8080/api/v1/users/$user_id/role" \
  -H 'Content-Type: application/json' --data '{"role":"admin"}'
curl -i -b "$cookie_jar" -X PATCH "http://127.0.0.1:8080/api/v1/users/$user_id/status" \
  -H 'Content-Type: application/json' --data '{"status":"disabled"}'

curl -i -b "$cookie_jar" -X POST http://127.0.0.1:8080/api/v1/auth/logout
rm "$cookie_jar"
```
