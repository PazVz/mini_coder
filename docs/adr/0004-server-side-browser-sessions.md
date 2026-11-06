# ADR-0004：浏览器采用服务端 Session + Cookie

- 日期：2026-11-06
- 状态：已采纳；尚未实现

## 背景

浏览器需要登录后管理工作区、连接终端，并在后续访问 IDE。项目需要实现注销、会话过期、账号停用和权限变更。用户采纳服务端 Session + Cookie 方案。

## 决定

服务端持久化会话，浏览器通过 HttpOnly Cookie 携带随机、不透明的会话标识。服务端据此解析用户并检查会话有效期，资源授权使用当前 RBAC 权限及资源归属。Cookie 的 Secure、SameSite 与 HTTP 请求的 CSRF 保护一起实现。

退出或停用账号时撤销相应会话；会话失效或授权撤销时，主动关闭关联的终端及后续 IDE 长连接。会话管理库与存储实现随认证功能选择。操作边界见 [工作区访问设计](../workspace-access.md)，实现参考 [OWASP 会话管理](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)。

## 备选方案

JWT 也可用于浏览器并放入 Cookie。仅离线验签的方案要另外处理有效期内的撤销与权限变化；当前单体服务采用共享会话状态更直接。后续跨服务或 agent 的短期访问凭据可以独立评估 JWT。

## 后果

登录状态需要服务端存储，身份与权限保持独立。测试覆盖会话创建、过期、注销、账号停用及相关长连接关闭。
