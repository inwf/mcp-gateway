# mcphub 使用指南

mcphub 是一个 MCP 网关：它连上若干个上游 MCP 服务器，把它们的工具与资源
合并成**一个** MCP 端点对外提供。客户端只需要配置一次，之后增删上游都不用
再动客户端。

## 目录

- [启动](#启动)
- [把客户端接上来](#把客户端接上来)
- [添加上游服务器](#添加上游服务器)
- [网关自带的系统工具](#网关自带的系统工具)
- [会话模式](#会话模式)
- [文件放在哪](#文件放在哪)
- [命令速查](#命令速查)

## 启动

```
mcphub serve
```

不带子命令的 `mcphub` 与 `mcphub serve` 等价。默认监听 `127.0.0.1:7788`，
一个端口同时提供三样东西：

| 路径   | 用途                                  |
| ------ | ------------------------------------- |
| `/mcp` | 聚合后的 MCP 端点，客户端连这里        |
| `/api` | 管理 API，Web 界面与 CLI 用它          |
| `/`    | Web 界面（构建时嵌入的话）             |

首次启动不需要配置文件，它会以默认值运行；等你配置了第一台服务器才会写出
`config.yaml`。

只想换一次地址，不动配置文件：

```
mcphub serve --port 9000
```

`--host` / `--port` 压过 `listen.host` / `listen.port`，**只影响本次运行**。
`--port 0` 表示向操作系统要一个空闲端口，实际端口在启动输出里报出。
`--host` 设成回环之外的地址，等于把管理 API 也一起暴露出去——它能改变哪些命令
会被当作子进程执行，所以那应当是一个明确的决定。

改配置前可以先离线检查一遍，这一步不需要网关在跑：

```
mcphub config validate
```

## 把客户端接上来

mcphub 对外说的是 **streamable HTTP**。以 Claude Code 为例：

```
claude mcp add --transport http mcphub http://127.0.0.1:7788/mcp
```

其他客户端如果只认 JSON 配置，写成这样：

```json
{
  "mcpServers": {
    "mcphub": {
      "type": "http",
      "url": "http://127.0.0.1:7788/mcp"
    }
  }
}
```

上游服务器的工具会以 `服务器名_工具名` 的形式暴露出来（例如 `files_read`），
所以两台服务器各有一个 `read` 也不会撞名。

**但默认一个上游工具都不暴露。** 客户端一开始只看到网关自己的七个系统工具，
上游工具要在配置里逐个点名（`mcpServers.<名字>.exposedTools`，Web 界面的工具页
每个工具有一个开关）才会进入 `tools/list`。

这是有意的：上游的 schema 很占地方，一台服务器十几个工具、每个十几个参数，
全塞进每个客户端的上下文就是纯浪费。**没暴露不等于用不了**——模型可以用
`search_tools` 找、`get_tool` 取 schema、`call_tool` 调，需要时才付这份 token。
把常用的几个暴露出来、其余留给按需检索，是这个网关想要的用法。

想看有哪些还没暴露：

```
mcphub tools list --all
```

## 添加上游服务器

上游只有两种，对应配置里 `transport` 的两个取值：

| `transport`       | 含义                                     |
| ----------------- | ---------------------------------------- |
| `stdio`           | 由 mcphub 拉起一个子进程，走它的标准输入输出 |
| `streamable-http` | 连接一个已经跑在别处的服务                 |

拉起子进程的写法——命令放在 `--` 之后，这样它自己的参数不会被 mcphub 抢走：

```
mcphub servers add files -- npx -y @modelcontextprotocol/server-filesystem /tmp
```

连接已有服务的写法：

```
mcphub servers add remote --url https://example.com/mcp \
  --header "Authorization=Bearer <token>"
```

两种写法都不用写 `--transport`：给了命令就是 `stdio`，给了 `--url` 就是
`streamable-http`。

常用选项：

| 选项            | 说明                                     |
| --------------- | ---------------------------------------- |
| `--env K=V`     | 子进程的环境变量，可重复                  |
| `--header K=V`  | HTTP 请求头，可重复                       |
| `--tag K=V`     | 分组与筛选用的标签，可重复                |
| `--proxy URL`   | 走 HTTP 代理连接                          |
| `--timeout 30s` | 单次请求超时                              |
| `--disabled`    | 只写进配置，先不连                        |
| `--from-file f` | 从一份 YAML 片段读取完整定义              |

命令会等它连上再返回，所以启动失败当场就能看到，不用事后去翻。

改完之后：

```
mcphub servers list
```

## 网关自带的系统工具

除了转发上游的工具，mcphub 自己还提供七个工具。它们的用途是：**上游工具很多
时，让模型先找再调，而不是把几百个工具的完整 schema 一次性塞进上下文。**

| 工具                         | 用途                                       |
| ---------------------------- | ------------------------------------------ |
| `list_servers`               | 有哪些服务器，各自是什么状态                |
| `list_tools`                 | 某台服务器提供哪些工具（名字 + 一行描述）    |
| `search_tools`               | 按关键词跨全部服务器搜工具                  |
| `get_tool`                   | 取某个工具的完整输入 schema 与 annotations   |
| `call_tool`                  | 按服务器名 + 工具名调用                     |
| `list_tags`                  | 列出标签，用于按用途筛选服务器              |
| `update_server_description`  | 改写某台服务器的描述                        |

典型用法是三步：`search_tools` 找到候选 → `get_tool` 取 schema →
`call_tool` 调用。已经暴露出来的工具可以直接按 `files_read` 这样的名字调，
不必绕 `call_tool`；没暴露的就走这三步，`call_tool` 对两者都管用。

**一个工具没出现在 `tools/list` 里，不代表它调不了。** 默认一个上游工具都不
暴露，这是有意的（见上文的 `exposedTools`）；未暴露的工具照样能被
`list_tools` / `search_tools` 发现，也照样能用 `call_tool` 调。

`search_tools` 的多个词是**放宽**而不是收紧：命中词多的排在前面，某个词在所有
工具里都没出现时会单独报在 `unmatched` 里，而不是把结果清空。所以拿同一件事的
几种说法一起查是可以的。

**问网关它自己**：`list_tools` 与 `get_tool` 都接受服务器名 `mcphub`，
返回的就是这七个工具本身——

```
list_tools(server="mcphub")              这七个工具都有哪些
get_tool(server="mcphub", tool="call_tool")   call_tool 自己要什么参数
```

其它系统工具不需要服务器名，直接调即可；`update_server_description` 对
`mcphub` 会明确拒绝，因为网关自己不是被代理的服务器。

除了工具，还有两份资源值得先读：`hub://guide` 是本文，
`hub://servers/{名字}` 是某台服务器的状态加它全部工具的「名字 → 描述」——
一次读取就能看完一台服务器，不必逐个 `get_tool`。

这七个工具在 CLI 和 Web 界面里都单独成组，也可以直接调用：

```
mcphub tools show list_tools        看它要什么参数
mcphub tools call list_servers      直接调它
```

## 会话模式

- **stateful**（默认）：每个客户端一个会话，支持服务端推送，工具列表变化会
  主动通知客户端。
- **stateless**：每个请求独立处理，`GET` 与 `DELETE` 被拒绝，没有通知。

选择顺序是：客户端请求头 `X-MCP-Session-Mode` > 配置里按 User-Agent 匹配的
`gateway.sessionModeRules` > `gateway.defaultSessionMode`。

**一条注意事项**：`gateway.keepAlive` 开启时，网关会周期性地向客户端发 MCP
`ping`。这要求客户端维持那条可选的 SSE 长连接（`GET /mcp`）。绝大多数客户端
——包括 Claude Code——默认就会维持它。若你的客户端明确关掉了这条流，请把
`gateway.keepAlive` 设为 `0`，否则它的会话会被判定为失联而关闭。

## 文件放在哪

所有东西都在**数据目录**里，默认是当前工作目录下的 `./data`：

```
data/
  config.yaml       配置
  logs/mcphub.log   日志
```

改用别处：`--data-dir /path` 或环境变量 `MCPHUB_DATA_DIR`。
只想换配置文件：`--config /path/to/config.yaml`。

想确认到底读了哪些路径：

```
mcphub check
```

## 命令速查

```
mcphub serve                        启动网关（不带子命令时的默认动作）
mcphub serve --host H --port N      换一次监听地址，不写回配置文件
mcphub check                        校验配置并报告各项路径，不监听端口
mcphub config validate [file]       离线检查配置，一次报出全部问题

mcphub servers list [--verbose]     列出服务器与各自状态
mcphub servers add <name> ...       添加服务器
mcphub status                       运行中实例的概况：连上了几台、在提供什么、谁连着

mcphub tools list [--search 词]     列出/搜索网关提供的工具
mcphub tools list --all             连没暴露的一起列，并标出各自的对外名字
mcphub tools show <工具>            看一个工具的完整说明与输入 schema
mcphub tools call <工具> --arg k=v  调用一个工具

mcphub tags list [--server 名]      列出标签，以及各自被哪些服务器带着

mcphub ui [--print]                 用浏览器打开 Web 界面
mcphub guide                        输出本文档
mcphub version                      输出版本
```

以 `servers`、`tools`、`tags` 开头的命令，以及 `mcphub status` 与 `mcphub ui`，
都是**运行中实例的客户端**——它们通过管理 API 询问那个实例，因为只有它知道自己
实际连上了哪些上游。默认从配置里的 `listen` 取地址，也可以用
`--address host:port` 指定。
