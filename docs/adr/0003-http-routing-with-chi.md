# ADR-0003：使用 Chi 组织 HTTP 路由与中间件

- 日期：2026-11-06
- 状态：已采纳；已实现

## 背景

项目目前使用标准库 `http.ServeMux`。后续将增加用户、工作区和实时连接接口，学习目标也包括实际使用 Go Web 框架。用户明确选择 Chi。

## 决定

采用 [Chi](https://pkg.go.dev/github.com/go-chi/chi/v5) 组织路由、分组与中间件，处理函数继续使用标准 `http.ResponseWriter`、`*http.Request` 和 `context`。业务逻辑放在相应业务包中，HTTP 层负责协议适配。

## 备选方案

继续使用 `http.ServeMux` 可以满足当前路由需求；Chi 提供方便的路由组合，并符合学习框架的目标。

实现与公共错误契约见 [ADR-0008](0008-authentication-and-http-errors.md)。

## 后果

增加一个路由依赖，仍可使用标准 HTTP 测试工具。实现时保留健康检查、404、405 和 HEAD 的既有行为，并验证中间件与 WebSocket 升级兼容。终端连接单独管理超时、取消和退出流程。
