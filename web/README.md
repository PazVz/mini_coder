# mini_coder Web

Vue 3 + TypeScript + Vite 的认证与账号界面，复用 Go 的 Session Cookie API。

在仓库根目录运行：

```bash
nvm install
nvm use
make run       # 构建页面并启动 Go，访问 127.0.0.1:8080
make web-dev   # 另开终端，热更新入口 127.0.0.1:5173
```

运行前需要配置数据库并执行迁移。双端开发时，邮件 Public URL 指向前端的 5173 端口。

详细的运行方式、测试与代码阅读顺序见 [前端开发](../docs/frontend.md)。
