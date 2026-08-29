# 配置参考

配置是**一个 YAML 文件**，默认在 `<数据目录>/config.yaml`。

两条贯穿全文的规则：

- **时长写成带单位的字符串**，如 `30s`、`5m`、`2m30s`、`168h`，不写裸数字。单位
  永远不含糊。
- **不认识的键是错误，不是被忽略的东西。** 把 `port` 拼成 `prot` 会导致启动
  失败并指出位置，而不是悄悄用回默认值——后者要到你纳闷"我明明改了"的时候才
  会被发现。

改完之后可以先离线检查，这一步不需要网关在跑：

```
mcphub config validate
```

它一次报出全部问题，不是报一个停一个。

## 一份完整的默认配置

下面这份就是首次启动时生效的内容（没有配置文件时它不会被写出来，等你配置了
第一台服务器才会落盘）：

```yaml
version: 1
listen:
  host: 127.0.0.1
  port: 7788
logging:
  level: info
  format: console
  maxAge: 168h0m0s
  maxSizeMB: 50
  mcpWireDebug: false
  apiDebug: false
security:
  allowedNetworks:
    - 127.0.0.1/32
    - ::1/128
  maxConnections: 50
  maxConcurrentRequests: 50
  connectionTimeout: 30s
  idleConnectionTimeout: 5m0s
gateway:
  defaultSessionMode: stateful
  sessionModeRules: {}
  sessionTimeout: 30m0s
  notifyDebounce: 3s
  keepAlive: 30s
  keepAliveFailureThreshold: 3
startup:
  connectDelay: 3s
  maxRetries: 3
  retryBackoff: 5s
mcpServers: {}
```

## `version`

| 字段      | 默认 | 说明                                                  |
| --------- | ---- | ----------------------------------------------------- |
| `version` | `1`  | 配置的 schema 版本。存在的意义是让将来不兼容的改动能被**发现**，而不是被误读 |

## `listen`

一个端口同时提供 MCP 端点、管理 API 与 Web 界面。

| 字段   | 默认        | 说明                                         |
| ------ | ----------- | -------------------------------------------- |
| `host` | `127.0.0.1` | 监听地址                                      |
| `port` | `7788`      | 监听端口。`0` 表示向操作系统要一个空闲端口     |

**默认只监听回环地址是刻意的。** 管理 API 能改变哪些命令会被作为子进程执行，
所以让它可达必须是一个明确的决定，而不是默认值的副作用。

`port: 0` 时端口在启动时才确定、只在启动输出里出现，配置文件里查不到——此时
CLI 的客户端类命令需要 `--address host:port`。

## `logging`

日志文件写在数据目录下的 `logs/`。

| 字段           | 默认        | 说明                                    |
| -------------- | ----------- | --------------------------------------- |
| `level`        | `info`      | `debug` / `info` / `warn` / `error`      |
| `format`       | `console`   | `console`（给人看）或 `json`（给机器看） |
| `maxAge`       | `168h0m0s`  | 轮转后的日志保留多久                     |
| `maxSizeMB`    | `50`        | 单个日志文件多大时轮转                   |
| `mcpWireDebug` | `false`     | 记录双向的 MCP 原始报文。很吵，排查协议层问题时才开 |
| `apiDebug`     | `false`     | 记录管理 API 的请求与响应体              |

## `security`

| 字段                    | 默认                            | 说明                          |
| ----------------------- | ------------------------------- | ----------------------------- |
| `allowedNetworks`       | `["127.0.0.1/32", "::1/128"]`   | 允许连接的 CIDR 列表           |
| `allowedOrigins`        | *(不写)*                        | 额外允许打开事件流的浏览器来源  |
| `maxConnections`        | `50`                            | 同时打开的 TCP 连接上限         |
| `maxConcurrentRequests` | `50`                            | 同时处理的请求上限              |
| `connectionTimeout`     | `30s`                           | 单连接的无活动超时              |
| `idleConnectionTimeout` | `5m0s`                          | 长连接流的无活动超时            |

三处需要注意：

- **`allowedNetworks` 为空列表表示放行所有客户端**，与"这个键不写"（拿到默认的
  回环白名单）意思相反。这是本 schema 里少数几个"空值 ≠ 缺省"的地方之一。
- **`idleConnectionTimeout` 必须大于 `connectionTimeout`**，否则每个安静的客户端
  都会在流中途被断开。校验会拒绝违反此关系的配置。
- 单独写一个 `allowedOrigins` 的原因：WebSocket 握手不受普通请求那套跨源检查的
  保护。不加限制的话，你访问的任何网站上的页面都能连上你自己机器上的这个端口
  并读走全部事件。空列表表示"仅同源"，也就是自带 Web 界面所需要的；把前端跑在
  独立的开发服务器上，才是需要往这里加条目的场景。

## `gateway`

| 字段                        | 默认        | 说明                                       |
| --------------------------- | ----------- | ------------------------------------------ |
| `defaultSessionMode`        | `stateful`  | `stateful` 或 `stateless`                   |
| `sessionModeRules`          | `{}`        | 按 User-Agent 子串选择模式，见下             |
| `sessionTimeout`            | `30m0s`     | 空闲这么久的会话过期                        |
| `notifyDebounce`            | `3s`        | 合并上游工具/资源变化的抖动窗口              |
| `keepAlive`                 | `30s`       | 向客户端发存活探测的间隔。`0` 关闭            |
| `keepAliveFailureThreshold` | `3`         | 连续失败多少次判定会话失联                   |

`sessionModeRules` 的写法：

```yaml
gateway:
  sessionModeRules:
    stateless:
      - curl
      - python-requests
```

匹配不区分大小写。优先级：客户端请求头 `X-MCP-Session-Mode` > 这里的规则 >
`defaultSessionMode`。

**`keepAlive` 有一个前提需要知道。** 它是**服务端向客户端发 MCP `ping`**，这
要求客户端维持那条**可选的** SSE 长连接（`GET /mcp`）。绝大多数客户端默认都会
维持它。若你的客户端明确关掉了这条流，它的会话会被判定失联而关闭——此时应把
`keepAlive` 设为 `0`，改由 `sessionTimeout` 回收。

## `startup`

启动时如何把上游一台台带起来。

| 字段           | 默认  | 说明                                        |
| -------------- | ----- | ------------------------------------------- |
| `connectDelay` | `3s`  | 逐台连接之间的间隔，避免一次拉起大量子进程    |
| `maxRetries`   | `3`   | 连接失败重试次数                             |
| `retryBackoff` | `5s`  | 指数退避的基准间隔                           |

## `mcpServers`

以名字为键。名字会直接用在聚合后的工具名和资源 URI 里，所以有约束：

- 只能是 `^[A-Za-z0-9][A-Za-z0-9_-]*$`（首字符是字母或数字，其余可含下划线和
  连字符），最长 64 个字符。
- 不允许空格、点、斜杠——它们在工具名和 URI 路径里有别的含义；开头的连字符会被
  CLI 读成标志。

| 字段           | 适用          | 默认      | 说明                                        |
| -------------- | ------------- | --------- | ------------------------------------------- |
| `transport`    | 全部          | `stdio`   | `stdio` 或 `streamable-http`                 |
| `enabled`      | 全部          | `true`    | 为 `false` 时保留在配置和界面里，但不连接      |
| `description`  | 全部          | *(空)*    | 界面上显示，也会返回给模型帮助它选服务器       |
| `tags`         | 全部          | *(空)*    | 自由形式的键值元数据，用于分组与筛选，不影响路由 |
| `timeout`      | 全部          | `60s`     | 单次请求超时                                 |
| `exposedTools` | 全部          | *(空)*    | 只暴露列出的工具；空列表表示全部暴露           |
| `command`      | stdio         | —         | 要执行的程序，**必填**                        |
| `args`         | stdio         | *(空)*    | 命令行参数                                   |
| `env`          | stdio         | *(空)*    | 追加的环境变量。是**叠加**不是替换，子进程仍能拿到 `PATH`、`HOME` |
| `url`          | streamable-http | —       | 服务端点，**必填**                            |
| `headers`      | streamable-http | *(空)* | 每个请求都会带上的请求头，如 `Authorization`   |
| `proxy`        | streamable-http | *(空)* | 走 HTTP 代理连接。不写则沿用 `HTTP_PROXY` 等环境变量 |

**两种传输各自需要恰好一个"地址"，写错另一个是错误而不是被忽略：** stdio 必须有
`command` 且不能有 `url`/`proxy`；streamable-http 必须有 `url` 且不能有 `command`。
在 stdio 服务器上写 `url`，最可能的情况是传输类型选错了——静默丢弃它会让 mcphub
连到一个不是你想要的地方去。

两个例子：

```yaml
mcpServers:
  files:
    transport: stdio
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-filesystem"
      - /tmp
    timeout: 30s
    tags:
      用途: 文件

  remote:
    transport: streamable-http
    url: https://example.com/mcp
    headers:
      Authorization: Bearer <token>
    timeout: 60s
```

命令行等价写法见 `mcphub guide --section 添加上游服务器`。

## 数据目录

配置、日志与运行时状态都在数据目录里，**不会写到主目录**：

```
data/
  config.yaml
  logs/mcphub.log
```

按以下顺序决定它的位置：

1. `--data-dir` 参数
2. `MCPHUB_DATA_DIR` 环境变量
3. 当前工作目录下的 `./data`

只想换配置文件而不换数据目录：`--config /path/to/config.yaml`。

想确认实际用了哪些路径：`mcphub check`。
