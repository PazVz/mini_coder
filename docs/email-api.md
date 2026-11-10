# 邮箱验证、维护与注册

SMTP 为可选能力。不配置时，已有账号登录、用户管理和修改密码照常工作；公开注册默认关闭。设计及事务边界见 [ADR-0011](adr/0011-smtp-email-verification-and-registration.md)。

## 配置与启动

先按 [本地开发](development.md) 升级数据库，完成 [管理员初始化](auth-api.md)。在 `.env` 配置后重新导出环境变量并重启服务；程序不自动加载文件。

| 环境变量 | 作用 |
| --- | --- |
| `MINICODER_PUBLIC_URL` | 邮件链接的受信任站点 origin，例如 `https://mini.example.com`；不可包含路径、凭据、查询或片段。仅 localhost / 回环 IP 允许 HTTP |
| `MINICODER_REGISTRATION_ENABLED` | 默认 `false`；设为 `true` 且完成管理员初始化后开放公开注册 |
| `MINICODER_SMTP_HOST` | SMTP 主机名或 IP；留空关闭邮件能力，其余 SMTP 配置也须留空 |
| `MINICODER_SMTP_PORT` | 默认 `587`；TLS 直连通常使用 `465`，以服务商要求为准 |
| `MINICODER_SMTP_TLS` | 默认 `starttls`，必须成功升级 TLS；`tls` 从连接开始使用 TLS。两者验证服务器证书，最低 TLS 1.2 |
| `MINICODER_SMTP_USERNAME` / `MINICODER_SMTP_PASSWORD` | 成对配置，使用 TLS 内的 AUTH PLAIN；不需要认证的受信任中继可同时留空 |
| `MINICODER_SMTP_FROM` | 单个发件邮箱，可带显示名；须使用 SMTP 服务允许的地址 |

配置 SMTP 时必须同时提供 From 和 Public URL。启动只校验配置，实际发送时才连接 SMTP。每次发送总超时 5 秒，没有自动重试；返回 202 表示 SMTP 已接受邮件，不保证最终入箱。发送失败返回 503 并使本次证明失效；发送配额仍保留。修正配置后重新申请即可，不复用旧链接。

本地邮件捕获服务可使用 `MINICODER_SMTP_HOST=127.0.0.1`、实际监听端口和 `MINICODER_SMTP_TLS=plain`，用户名与密码留空；明文模式仅允许回环 IP。远程 SMTP 必须使用 TLS。公开注册页为 `/auth/register`；未启用注册时，提交申请返回 `registration_disabled`。

## API

所有写接口使用单个 JSON 对象、`Content-Type: application/json`，最大 16 KiB，拒绝未知字段；沿用同源 CSRF 防护和 [统一失败响应](auth-api.md#失败响应)。用户名、密码及邮箱规则见 [认证 API](auth-api.md)。

| 接口 | 请求 JSON | 成功结果 |
| --- | --- | --- |
| `POST /api/v1/auth/register/request` | `{"email":"alice@example.com"}` | 202，提交注册邮件 |
| `POST /api/v1/auth/register` | `{"token":"邮件令牌","username":"alice","password":"example-password-123"}` | 201，已验证邮箱的 member 用户；不自动登录 |
| `POST /api/v1/auth/email/verification` | `{}`，须登录 | 202，验证当前尚未验证的主邮箱 |
| `POST /api/v1/auth/email/change` | `{"email":"new@example.com","current_password":"example-password-123"}`，须登录 | 202，向新地址发信；也用于旧账号补录邮箱 |
| `POST /api/v1/auth/email/confirm` | `{"token":"邮件令牌"}`，须登录同一账号 | 200，更新后的用户 |

申请成功统一返回：

```json
{"message":"验证邮件已提交，请查收邮件；重新申请后请使用最新链接。"}
```

注册申请对已占用和未占用的邮箱执行相同发信流程，不返回账号是否存在。申请不创建用户、不保留用户名，也不占用正式用户的邮箱。持有邮件证明后才能提交用户名和密码，事务中创建 active member 并标记邮箱已验证；用户名或邮箱冲突返回 409，令牌在到期前仍可重试。注册接口不接受 email、role、status 或验证状态。

初始化和管理员创建的账号仍允许邮箱未验证；用户登录后申请验证即可。旧账号没有邮箱时，先调用 change 补录。修改邮箱需要当前密码和有效会话，确认前旧主邮箱及其验证状态保持不变；确认新邮箱后才通过数据库唯一约束原子替换。确认必须有该用户当前有效的会话，可以在同账号的另一设备完成；仅持有邮件链接不够。成功换址后向旧地址发送通知，通知失败记录告警但不撤回已完成的修改。验证或换址不退出已有会话。

数据库继续兼容无本地密码账号。这些账号可在有效会话下验证已有邮箱，不能使用要求当前密码的 change 接口补录或替换；未来 OIDC 接入时增加对应的重新认证方式。当前未提供无密码账号的登录入口。

## 链接、令牌与限流

邮件链接为 `/auth/register#token=...` 或 `/auth/email/confirm#token=...`。令牌只在浏览器片段中，HTTP GET 不携带；页面读取后移除地址栏片段，在用户提交表单时才 POST。GET / HEAD 不消费令牌，邮件扫描器预览不会完成注册或换址。页面不加载第三方资源，禁止缓存、嵌入及 Referrer 发送。刷新后需从原邮件重新打开链接。

令牌含 32 字节密码学随机数，数据库只存 SHA-256 哈希，30 分钟有效、一次使用，并绑定用途、目标邮箱及账号。注册令牌不能用于维护邮箱，维护令牌不能用于注册或另一账号。重新申请会替换同邮箱的旧注册令牌，或同用户的旧维护令牌。改密、修改主邮箱、停用账号同时使该账号待处理的维护令牌失效。

所有邮件 API 与初始化、登录、创建用户、改密共享进程内限流：每个来源 IP 每分钟 10 次，全进程每分钟 60 次，最多 4 个并发请求。发证明邮件还受数据库配额约束：目标邮箱忽略 ASCII 大小写，跨用途、跨进程共享每分钟一次、每小时窗口最多五次。窗口从首次申请开始；令牌消费、重发、发送失败或进程重启不重置配额。429 返回 `Retry-After` 秒数。旧地址变更通知不占证明邮件配额。

过期判断使用数据库当前时间，服务每小时清理过期令牌和已结束的配额窗口；正确性不依赖清理是否及时执行。日志不记录密码、原始令牌、完整验证链接、邮件正文或 SMTP 原始回复。反向代理也应避免记录这些 API 的请求体。

## 失败响应

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | `validation_failed` | 邮箱、用户名或密码不合法 |
| 400 | `invalid_email_token` | 格式错误、过期、重放或令牌用途/账号不符 |
| 401 | `unauthenticated` | 维护邮箱需要登录，或等待用户锁期间会话已失效 |
| 403 | `registration_disabled` | 注册开关关闭或管理员尚未初始化 |
| 403 | `invalid_current_password` | 申请换址时当前密码错误 |
| 409 | `username_taken` / `email_taken` | 完成注册或确认邮箱时遇到数据库唯一冲突 |
| 409 | `email_missing` | 验证当前邮箱时账号尚无邮箱，应先补录 |
| 409 | `email_already_verified` | 当前邮箱已验证 |
| 409 | `password_not_set` | 无本地密码账号不能使用密码确认换址 |
| 429 | `rate_limited` | IP、进程、并发或目标邮箱配额受限 |
| 503 | `email_unavailable` | SMTP 未配置 |
| 503 | `email_delivery_failed` | 发信未成功，应在冷却后重新申请 |

数据库写入失败会回滚令牌消费、创建用户或邮箱更新；内部错误不回显数据库及 SMTP 详情。

## 本地操作示例

完成 SMTP 配置并开启注册后，浏览器打开 `http://127.0.0.1:8080/auth/register`，输入可收信地址，从邮件链接设置用户名和密码。注册完成后按 [登录示例](auth-api.md#本地操作) 登录。

已有用户可使用登录得到的 Cookie jar（以下为 `cookies.txt`）申请验证或换址：

```bash
curl -i -b cookies.txt http://127.0.0.1:8080/api/v1/auth/email/verification \
  -H 'Content-Type: application/json' -d '{}'

curl -i -b cookies.txt http://127.0.0.1:8080/api/v1/auth/email/change \
  -H 'Content-Type: application/json' \
  -d '{"email":"new@example.com","current_password":"example-password-123"}'
```

在已有该账号登录 Cookie 的浏览器打开邮件链接，点击确认邮箱。curl 的 Cookie jar 与浏览器独立；使用 curl 时，取邮件链接 `#token=` 后的值填入下列 JSON，经同一 Cookie jar 确认：

```bash
curl -i -b cookies.txt http://127.0.0.1:8080/api/v1/auth/email/confirm \
  -H 'Content-Type: application/json' -d '{"token":"替换为邮件中的令牌"}'
```

当前只提供注册和邮件确认的最小页面，完整登录、账号设置与 Vue 界面仍待实现。密码找回和 OIDC 不在本次 API 范围内。
