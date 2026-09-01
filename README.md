# mcphub

把若干个 MCP 服务器合并成**一个**端点。

客户端只配置一次，之后增删上游、改配置、看日志都在网关这边完成，不用再碰
客户端。

```
Claude Code ─┐                        ┌─ stdio 子进程（filesystem、git…）
其他客户端  ─┼─→  mcphub  /mcp  ──────┼─ stdio 子进程
             ┘                        └─ streamable HTTP 服务（远端）
```

## 它解决什么

一个 MCP 客户端接十几个服务器时，会遇到三件麻烦事：

- **每个客户端都要配一遍。** 换一台机器、换一个客户端，就要重来。
- **上下文被工具塞满。** 几百个工具的完整 schema 一次性进上下文，代价不小。
- **出了问题看不见。** 某台服务器起不来时，客户端通常只是"那个工具不见了"。

mcphub 对应的做法是：客户端只配一个地址；提供 `search_tools` / `get_tool` /
`call_tool` 这类系统工具，让模型**先找再调**而不是全量加载；并给出一个 Web 界面
和一套 CLI，能看到每台服务器的状态、日志和它报的错。

## 快速开始

需要 Go 1.25+ 和 Node 22+（含 pnpm）。

```
make build          # 构建前端，嵌入后端，产出单个二进制
./backend/bin/mcphub serve
```

打开 <http://127.0.0.1:7788/> 是 Web 界面，把客户端指向
<http://127.0.0.1:7788/mcp>。

以 Claude Code 为例：

```
claude mcp add --transport http mcphub http://127.0.0.1:7788/mcp
```

加一台上游服务器：

```
mcphub servers add files -- npx -y @modelcontextprotocol/server-filesystem /tmp
mcphub servers list
```

完整用法：`mcphub guide`。配置字段：[docs/configuration.md](docs/configuration.md)。
网关对模型说了什么：[docs/mcp-surface.md](docs/mcp-surface.md)。

## 有什么

- **一个 MCP 端点**聚合全部上游。工具以 `服务器名_工具名` 暴露，两台服务器各有
  一个 `read` 也不会撞名。
- **两种上游传输**：`stdio`（由 mcphub 拉起子进程）与 `streamable-http`（连接
  已在运行的服务）。
- **七个系统工具**，让模型按需检索工具而不是全量加载。
- **Web 界面**：服务器状态、工具与资源浏览、实时日志、配置编辑。
- **CLI**：`servers` / `tools` / `tags` / `ui` / `config validate` / `guide`。
- **单个二进制**。前端在构建时嵌入，部署时不需要另外准备静态文件。
- **所有产生的文件都在数据目录内**（默认 `./data`），不往主目录里散落东西。

## 构建与开发

```
make help           # 列出全部目标
make build          # 前端 → 嵌入 → 单个二进制
make check          # 两侧的全部检查
make dev            # 打印开发时怎么把两半跑起来
make clean          # 清理构建产物
```

开发时前后端分开跑：

```
cd backend  && go run ./cmd/mcphub serve
cd frontend && pnpm dev
```

开发服务器会把 `/api`、`/ws`、`/mcp` 代理到网关，并**保留页面自身的来源**——
这样网关的 WebSocket 来源检查是被真正走了一遍，而不是被绕过去。

`make check` 包含：

| 一侧 | 内容                                             |
| ---- | ------------------------------------------------ |
| Go   | `gofmt` 检查、`go vet`、`go test -race ./...`      |
| Web  | TypeScript 类型检查、oxlint、Vitest               |

**Web 界面是构建期的选择，不是运行期的。** 后端用 `webui` 构建标签决定是否嵌入
前端；不带这个标签构建出来的二进制照常提供 API 与 MCP 端点，只是没有界面。
`make build` 会带上它。

## 目录结构

```
backend/
  cmd/mcphub/        CLI：命令树、serve、各子命令、内嵌使用指南
  internal/
    config/          配置的类型、解析、校验、读写
    logging/         日志、轮转、供界面查询的内存缓冲
    upstream/        上游连接：拨号、状态、重连
    gateway/         聚合的 MCP server、系统工具、会话模式
    api/             管理 API、WebSocket 事件流、静态资源
    events/          事件总线
    webui/           嵌入前端产物（受构建标签控制）
    testmcp/         测试用的真实 MCP 服务器，两种传输共用
    integration/     跨包的端到端测试
frontend/
  src/
    api/             HTTP 客户端、事件流、类型
    pages/           各页面
    components/      共用组件
    layout/          外壳与导航
    hooks/ stores/   事件流订阅与客户端状态
    theme/ styles/   设计令牌与主题
    i18n/ lib/       文案与工具函数
    test/            测试夹具
docs/
  configuration.md   配置字段参考
  mcp-surface.md     网关对 MCP 客户端的自述
```

## 技术选型

**后端** Go 1.25 · [官方 MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
· gin · gorilla/websocket · goccy/go-yaml · cobra · 标准库 `log/slog`

**前端** React 19 · TypeScript 6（`strict` 及五个附加严格标志全部显式开启）·
Vite 8 · antd 6 · TanStack Query 5 · Zustand 5 · React Router 7 · Vitest + msw ·
oxlint
