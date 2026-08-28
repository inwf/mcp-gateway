# AGENTS.md

给在这个仓库里工作的 AI 助手看的。面向使用者的文档在
[README.md](README.md)、[docs/configuration.md](docs/configuration.md) 和
`mcphub guide`。

## 一句话

Go 后端 + React 前端的 MCP 网关，构建成单个二进制。`backend/` 和 `frontend/`
是两个平级项目，`Makefile` 在仓库根负责把它们串起来。

## 开始之前

```
make check      # Go：gofmt + vet + test -race；Web：typecheck + oxlint + vitest
make build      # 前端 → 拷进嵌入目录 → 带 webui 标签构建二进制
```

**提交前 `make check` 必须全绿。** 两侧都要。

只跑一侧：`make check-backend` / `make check-frontend`。

## 这个仓库里那些不显然的约定

### 配置

- **时长在 JSON 上是字符串**（`"30s"`），不是纳秒数字。管理 API 刻意保持配置
  文件的形状——`internal/api/configjson.go` 用 YAML 做了一层桥接。碰 API 的响应
  类型时先看它。
- **解析是严格的**：未知键会报错并指出行号。这是刻意的——拼错的键静默取默认值，
  要到用户纳闷"我明明改了"的时候才会被发现。
- **`time.Time` 用 `omitzero` 而不是 `omitempty`。** 后者对结构体永远不生效，
  会把零值时间发成公元 1 年。

### 后端

- 上游传输只有两种：`stdio` 与 `streamable-http`。校验里用一个 `spawns` 布尔量
  就能穷尽。
- `Status.PID` 与 `StartedAt` 只对拉起子进程的传输有意义，HTTP 上游必须缺省而
  不是填 0 和公元 1 年。
- CLI 里以 `servers`、`tools` 开头的命令是**运行中实例的 HTTP 客户端**。它们
  不自己加载配置去连上游——那样报的是"第二种意见"而不是事实。
- 退出码分三种：0、1（运行失败）、2（用法错误）。cobra 不区分后两者，靠
  `cmd/mcphub/root.go` 里的 `usageError` 包装类型分流。

### 前端

- 包管理器是 **pnpm**，lint 用 **oxlint**（不是 ESLint）。
- TypeScript 开了 `noUncheckedIndexedAccess`、`exactOptionalPropertyTypes`、
  `erasableSyntaxOnly` 等。可选属性通常需要显式写 `| undefined`；构造函数参数
  属性不能用。
- 图标会污染按钮的可访问名（antd 的图标带 `role="img" aria-label`）。只有图标的
  按钮统一走 `components/IconButton.tsx`，装饰性图标加 `aria-hidden`。
- `ConfigProvider` 上设了 `button={{ autoInsertSpace: false }}`——否则 antd 会在
  两个汉字之间插空格，按钮的可访问名会变成"创 建"。

### 互操作（重要）

**Go 字段写成 `any` 会让 SDK 生成 JSON Schema 的 `true`。** 那是合法 schema，但
TypeScript 的 MCP SDK 会把 `properties` 下的每一项按对象校验，遇到布尔就**拒绝
整个 `tools/list` 响应**——一个字段能让全部工具对 Claude Code 之类的客户端集体
消失。

`internal/gateway/interop_test.go` 锁住了这一类问题。**跨语言的互操作，同语言的
测试再多也覆盖不到**，必要时拿对端 SDK 实测一次。

## 测试的写法

- **测外部行为，不测实现细节。** 断言的是使用者看得见的东西。
- **测试名是一句话**，说明它保护的是什么行为，不是它调用了哪个函数。
- **注释写"为什么"。** 不显然的断言旁边应当说明它防的是什么。
- **防漂移的测试要做反向验证。** 一条永远绿的测试和没有测试是一回事——写完之后
  故意把被测行为改坏一次，确认它真的红。`cmd/mcphub/guide_test.go` 是这类测试
  的例子：它拿使用指南对照命令树、系统工具名和各项常量，而不是对照一份固定文本。
- 需要真实 MCP 服务器时用 `internal/testmcp`：`ServerConfig` 给 stdio 版本
  （重新执行测试二进制自己），`Handler` 给 HTTP 版本。两者共用同一个被测服务器，
  不会各自漂移。
- 测试**不要绑定固定端口**。用端口 0 加 `serveOptions.Ready` 回调拿实际地址；
  绑 7788 的测试会在开发者自己跑着一个实例时失败。

## 提交

- 一次提交是一个完整的改动单元，不是一个步骤。
- 提交前 `make check` 全绿。
- 运行时产生的文件（`data/`、构建产物、`node_modules/`）都在 `.gitignore` 里，
  不要提交。
