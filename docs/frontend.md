# Vue 前端开发

前端位于 `web/`，实现管理员初始化、邮箱注册、登录、账号资料、邮箱验证与换址、退出。后端继续执行所有认证和授权规则。设计见 [ADR-0012](adr/0012-vue-authentication-interface.md)。

## 环境与版本

使用 `.nvmrc` 固定的 Node `26.13.1` LTS，随该版本提供的 npm `11.20.0`。通过 nvm 安装并切换：

```bash
nvm install
nvm use
```

2026-11-17 核实官方发布及 npm 注册表，以下直接依赖固定在 `web/package.json`，间接依赖固定在 `web/package-lock.json`：

| 工具 | 版本 | 官方来源 |
| --- | --- | --- |
| Node | 26.13.1 LTS | [发布说明](https://nodejs.org/en/blog/release/v26.13.1) |
| create-vue（初始化工具） | 3.24.0 | [官方脚手架](https://github.com/vuejs/create-vue) |
| Vue / Vue Router | 3.5.45 / 5.5.1 | [Vue](https://github.com/vuejs/core/releases)、[Router](https://github.com/vuejs/router/releases) |
| Vite / plugin-vue | 8.3.8 / 6.0.11 | [Vite](https://github.com/vitejs/vite/releases)、[Vue 插件](https://github.com/vitejs/vite-plugin-vue/releases) |
| TypeScript / vue-tsc | 6.0.3 / 3.3.14 | [TypeScript](https://github.com/microsoft/TypeScript/releases)、[Vue 语言工具](https://github.com/vuejs/language-tools/releases) |
| Vitest / jsdom | 5.1.0 / 31.0.1 | [Vitest](https://github.com/vitest-dev/vitest/releases)、[jsdom](https://github.com/jsdom/jsdom/releases) |
| Playwright | 1.65.1 | [发布说明](https://github.com/microsoft/playwright/releases) |
| Prettier | 3.10.1 | [发布说明](https://github.com/prettier/prettier/releases) |

TypeScript 7.0.2 与当前 vue-tsc 的编译入口不兼容，因此使用兼容的稳定版 6.0.3。`@types/node` 固定为 26.6.6。依赖查询来源为各包官方发布的 [npm 注册表](https://registry.npmjs.org/)。

## 两种运行方式

首次启动前，按 [本地开发](development.md) 配置 `.env`、启动数据库并执行迁移。HTTP 本地开发设置 `MINICODER_COOKIE_SECURE=false`。

**直接体验完整界面**：

```bash
set -a
. ./.env
set +a
make run
```

`make run` 自动准备 npm 依赖、构建 Vue，然后启动 Go。浏览器访问 `http://127.0.0.1:8080`。此模式的 `MINICODER_PUBLIC_URL` 也使用 `http://127.0.0.1:8080`。

**修改 Vue 时使用热更新**：Go 服务保持运行，另开终端：

```bash
nvm use
make web-dev
```

浏览器访问 `http://127.0.0.1:5173`。Vite 把 `/api` 请求转发给 Go，并保留 Host，Cookie 与同源保护继续生效。测试邮件流程时，将 Go 服务的 `MINICODER_PUBLIC_URL` 改为 `http://127.0.0.1:5173`，重新加载配置并重启 Go。统一使用 `127.0.0.1`，避免与 `localhost` 混用。

服务端 SMTP、数据库和初始化令牌保存在根目录 `.env`，Vite 使用 `web/` 为工程根目录。不要把这些凭据放入 `VITE_*` 变量；它们会进入浏览器构建结果。Vite 的 API 代理只转发 HTTP，不代理 Go 的 SMTP 连接。

## 首次初始化与注册

1. 为 Go 配置至少 32 字节的 `MINICODER_SETUP_TOKEN`，例如使用 `openssl rand -hex 32` 生成；重新加载配置后启动服务。
2. 浏览器打开首页，未初始化时进入 `/auth/setup`。手动填写令牌、管理员用户名、邮箱和密码，完成后前往登录。初始化完成后可移除服务端初始化令牌并重启。
3. 配置 SMTP、Public URL，并设置 `MINICODER_REGISTRATION_ENABLED=true`。使用另一个未占用的邮箱进入 `/auth/register` 申请注册。
4. 从邮件打开链接，填写用户名和密码。成功后使用用户名登录，进入 `/account` 查看已验证的邮箱；退出后再次访问该页面会进入登录页。
5. 已有用户可在账号页面验证当前邮箱或申请换址；打开维护邮件时如果尚未登录，可以在确认页直接登录，再明确确认邮箱。

邮件片段在页面启动时清除。刷新或离开表单后要从原邮件重新打开；重发使旧令牌失效。注册、SMTP、唯一邮箱、限流与错误码规则见 [邮箱 API](email-api.md)。

## 阅读代码

| 入口 | 内容 |
| --- | --- |
| `web/src/main.ts`、`emailProof.ts` | 清除邮件片段，再创建 Vue 应用与路由 |
| `web/src/router/index.ts` | 页面路径 |
| `web/src/views/` | 按操作组织的页面 |
| `web/src/components/LoginForm.vue` | 登录页和邮箱确认页复用的表单 |
| `web/src/api/` | HTTP 请求、用户类型和后端契约 |
| `web/src/composables/` | 内存中的会话、提交状态与重发冷却 |
| `internal/httpapi/frontend.go`、`web/embed.go` | Go 提供页面与构建资源 |
| `web/e2e/auth.spec.ts` | 浏览器完整流程 |

组件使用 Composition API 与 `<script setup lang="ts">`。字段名保持后端的 `snake_case`；页面依赖明确的小函数，使用原生 fetch。格式化运行 `npm --prefix web run format`。

## 验证与构建

```bash
make web-check
make check
```

`web-check` 包含类型检查、生产构建、格式检查和 Vitest；`check` 还包含 Go 静态分析、单元与真实数据库集成测试以及二进制构建。`make` 通过锁文件维护安装依赖；手动安装使用 `npm --prefix web ci`。

浏览器测试另需 Chromium，只需首次安装：

```bash
cd web
npx playwright install chromium
cd ..
make test-browser
```

`test-browser` 要求已导出 `MINICODER_TEST_DATABASE_URL`，使用真实 Go 服务与独立 schema，通过测试收件箱读取邮件链接，不发送外部邮件。已有 Chromium/Chrome 时可设置 `CHROMIUM_PATH` 为其可执行文件路径。CI 安装 Chromium 和系统依赖后执行同一入口。

`make build` 和发布脚本都会先构建前端，产物嵌入服务二进制；运行发布包不需要 Node 或独立的 `dist` 目录。直接执行 Go 命令前先运行 `make web-build`，否则缺少 embed 资源。新增页面时，同步 `frontend.go` 与 Vue 路由清单；未知 API、静态资源及路径保持 404。
