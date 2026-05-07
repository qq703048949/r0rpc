# R0RPC 项目完整记录

## 1. 记录说明

这份文档用于整理本次围绕 R0RPC 项目的完整沟通、设计、实现和部署过程，方便后续自己维护、交接、扩展，或回头查设计决策。

说明如下：

- 本文档不是逐字聊天导出，而是按主题和时间线整理后的完整项目纪要。
- 你在对话中提供过真实的 MySQL / Redis 连接信息。
- 按你的要求，本文档中所有真实数据库与 Redis 配置均已脱敏，不展示真实主机、端口、密码、库名细节。
- 实际可运行配置已经写入项目本地配置文件，文档里只保留占位说明。

项目根目录：`D:\r0rpc`

## 2. 项目背景

本次项目目标，是参考 Sekiro RPC 的使用方式和整体思路，重新做一套可自己掌控、可管理、可统计、可扩展、可部署的 RPC 系统。

你最初希望达到的核心目标包括：

1. 后端可登录，并支持账号管理。
2. 可新增账号，并控制账号是否允许访问。
3. 可监控不同 group 的访问情况，并支持按 group / action / client 过滤查询最近数据。
4. 原始数据只保留 3 天即可。
5. 需要统计性能指标，例如每个设备一周内的请求数、成功率等。
6. 需要真正的后端管理界面，不是只有接口。
7. 需要 Python demo。
8. 需要 Java / Xposed demo，并最终能打成 jar 给 Xposed 工程引用。
9. 需要最终能在 Linux 上用 Docker 一键部署。
10. 希望整体调用体验尽量接近 Sekiro 原版。

## 3. 需求演进过程

本次实现过程中，需求不是一次性定死的，而是逐步明确和收敛，主要变化如下。

### 3.1 数据库要求调整

一开始你给出了现有数据库配置，其中默认数据库名是 `yxz`。

随后你明确说明：

- `yxz` 是你已经在用的数据库。
- 新项目不能直接占用这个已有库。
- 需要单独新建一个数据库。
- 最终决定项目数据库名改为 `r0rpc`。

因此本项目后续统一围绕 `r0rpc` 数据库设计表结构与初始化逻辑。

### 3.2 后台界面要求不断增强

你对后台管理界面的要求逐步增加，经历了下面这些阶段：

1. 先要求“有真正的后端管理界面”。
2. 然后要求把请求量、成功率等做成图表，并支持一周内统计。
3. 要求支持按 client / group 等维度过滤。
4. 要求菜单化，不要把所有信息挤在一个页面里。
5. 要求 Group / Client 浏览体验更清晰。
6. 要求 Group 管理页支持分页和排序。
7. 要求 Client 页面更像左侧树状目录。
8. 要求点击某个 client 后，右侧能看到最近请求和最近调用结果。
9. 要求没有设备的 group 做额外标识。
10. 要求请求记录里的 group / action / clientId 不要手填，改为下拉可选。

### 3.3 在线状态与刷新问题

你在使用过程中发现：

- Group 页面里的在线状态会在切换页面后显示不准。
- 设备明明在线，但 Group 列表里会变成离线。
- 只有手动刷新后才恢复正常。

因此后续把在线状态判断与页面刷新逻辑作为重点修正项之一。

### 3.4 RPC 语义要求更像 Sekiro

项目最开始虽然已经能工作，但你提出一个关键点：

- Sekiro 的 handler 风格是 `handleRequest(SekiroRequest request, SekiroResponse response)`。
- `SekiroResponse.success(...)` 与 `fail(...)` 的语义非常直接。
- 你希望新的 Java / Xposed 客户端也尽量保留这种编程体验。

你还特别强调：

- 在 Xposed 里写 handler 时，希望像 Sekiro 一样直接继承类、写 `handleRequest`。
- 不希望每个业务 handler 自己手动写一层 `try/catch`。
- 即使 handler 里报错，也要自动兜底返回失败信息。
- 失败结果里最好包含错误内容，便于定位问题。

这个要求后来变成了 Java client 重构的核心方向。

### 3.5 传输模式从 long-poll 转向 WebSocket

项目过程中，我们专门讨论过：

- long-poll 版 client 的意义是什么。
- Python 普通调用端和“设备端长期在线监听任务”的角色如何对应。
- 原版 Sekiro 的工作方式在你的理解中，是服务端起来后，Python 直接请求即可。

随后你继续追问 long-poll 和 WebSocket 的优缺点，并最终明确要求：

- 改成 WebSocket 方案。
- 要追求极致性能。

因此项目最终主链路以 WebSocket 作为设备端常驻通道，HTTP 作为普通 invoke 调用入口。

### 3.6 部署与配置方式调整

一开始项目兼容环境变量读取，后续你明确要求：

- 不要通过 `getenv` 到处取配置。
- 不管 Windows 还是 Linux，都统一读取某个配置文件。
- 配置最好写死到文件里。

因此最终调整为：

- 本地 / Windows 默认读取 `r0rpc.conf`
- Linux Docker 运行时也挂载同名配置文件到容器中使用

### 3.7 数据保留与时区要求

你发现之前“原始请求保留 3 天、聚合指标保留更久”的说法还不够明确，于是重新确认规则：

1. 原始明细保留 3 天。
2. 聚合统计表保留 1 个月。
3. 系统里时间差了 8 小时，需要修正时区。

因此最终统一成：

- 原始请求表保留 3 天。
- 聚合统计表保留 30 天。
- 应用与数据库会话统一按 `Asia/Shanghai` 处理。

## 4. 原始 Sekiro 方案的不足

在项目早期，我们先讨论了“RPC 本身有什么缺点”，核心结论包括：

1. 原版 Sekiro 更偏向一个轻量转发工具，不是完整的业务平台。
2. 原版在后台账号管理、权限控制、禁用账号、设备分组治理等方面能力较弱。
3. 原版监控和运营分析能力有限，缺少图形化统计、趋势图、维度筛选和长期聚合分析。
4. 原版对部署、持久化、审计、后台运营并不友好，更偏向“能转发调用”。
5. 当设备量、group 数量、调用频率都上来之后，缺少完整的可观测性会让排障和运营很吃力。
6. 如果只保留原始请求而不做聚合统计，历史趋势分析会变差，库表压力也会偏大。

基于这些讨论，项目后续方向就从“只做一个可调用的 RPC”扩展成“可管理的 RPC 平台”。

## 5. 最终整体设计思路

### 5.1 架构总览

项目最终形成的结构是：

- Go 后端：负责鉴权、调度、管理后台、统计、设备管理、请求路由。
- WebSocket 设备端通道：手机 / Xposed / Java client 常驻连接到服务端，等待服务端下发任务。
- HTTP invoke 入口：Python 或其他调用方通过 HTTP 发起调用。
- MySQL：存储账号、设备、group、请求明细、聚合统计等数据。
- Redis：用于缓存、会话加速、在线态辅助、调度相关临时数据。
- 前端管理界面：内嵌到 Go 服务中，直接由后端提供页面和接口。
- Java client jar：提供接近 Sekiro 的 handler 编程方式。
- Xposed demo：展示如何在实际设备侧注册 action 并处理请求。
- Python demo：展示如何发起 invoke 调用，以及如何模拟设备端。

### 5.2 传输设计

最终采用的是：

- 设备端：WebSocket 长连接
- 调用端：HTTP

这样做的原因是：

1. 设备端长期在线时，WebSocket 的连接复用效率更高。
2. 对服务端来说，向设备投递任务更直接，不用反复轮询。
3. 并发量上来后，WebSocket 比 long-poll 更节省连接切换成本。
4. Python 调用方仍然可以保持简单，直接发 HTTP 请求即可。

### 5.3 为什么保留 long-poll demo

虽然最后主链路切到 WebSocket，但项目里仍保留了 `client_long_poll.py`，它的意义主要是：

- 帮助理解“设备端常驻监听任务”的角色。
- 保留对早期思路的示例说明。
- 方便对比 long-poll 和 WebSocket 两种模式。

你后续的理解已经非常准确：

- `client_long_poll` 或 `client_websocket` 相当于“手机端 / 设备端”，持续监听任务。
- `invoke_demo.py` 相当于普通业务请求方，比如 Python 脚本发起 RPC 调用。

## 6. R0RPC 相比原生 Sekiro 的优缺点

### 6.1 R0RPC 的优势

1. 有完整后台，不只是一个转发中枢。
2. 支持登录、账号管理、账号启停、权限控制基础能力。
3. 支持 group / client 维度管理。
4. 支持请求明细查询与聚合图表。
5. 支持设备一周维度统计与成功率分析。
6. 支持原始请求与聚合指标分层保留策略。
7. 支持部署到 Linux Docker。
8. 支持基于文件的统一配置方式。
9. Java / Xposed 侧逐步贴近 Sekiro 的 handler 风格。
10. 错误返回和调用记录更适合排障。

### 6.2 R0RPC 相比原生 Sekiro 的代价

1. 系统更重，组件更多。
2. 运维复杂度更高，需要 MySQL、Redis、后端服务等配合。
3. 管理能力增强后，开发和维护成本也更高。
4. 如果只是做极简转发，原版会更轻。

### 6.3 与原版最接近的部分

你最关心的是调用风格。为此项目重点做了两件事：

1. 设备端仍然采用“注册 action，收到请求后回调 handler”的思路。
2. Java client 提供 `SekiroRequest` / `SekiroResponse` / `ActionHandler` 风格，让 Xposed 侧写法尽量像原版。

## 7. 并发与“请求不会错位”问题

你明确问过一个很关键的问题：

- 当大量并发请求同时存在时，任务和返回会不会错位。
- 如果设备端连续 success 多次，会不会影响真实返回。

这部分最终设计原则是：

1. 每个调用都有独立请求 ID。
2. 服务端下发任务时，请求 ID 跟随任务一起发到设备端。
3. 设备端返回结果时，仍然带回对应请求 ID。
4. 服务端只会把结果归并到对应的那个请求上。
5. 同一个请求结果只允许第一次成功写回生效。
6. 后续重复的 `success` / `failed` 会被忽略，避免污染最终响应。

这和你担心的“结果错位”正好对应，是系统正确性里的核心部分。

## 8. Java / Xposed 侧的最终改造方向

### 8.1 你的目标

你给出的目标非常明确：

- 希望像 Sekiro 一样，业务代码只需要写一个 handler 类。
- 类里实现 `action()` 和 `handleRequest(...)`。
- 业务里直接 `response.success(ret)`。
- 如果执行过程报错，希望框架自动兜底返回失败，而不是要求每个 handler 手写 `try/catch`。

### 8.2 最终 Java API 形态

围绕这个目标，项目补齐并重构了以下接口和类：

- `ActionHandler`
- `SekiroRequest`
- `SekiroResponse`
- `@AutoBind`
- `SekiroLikeClient`

主要效果是：

1. 可以直接 `registerHandler(ActionHandler handler)`。
2. 每个 handler 通过 `action()` 声明 action 名。
3. 通过 `handleRequest(SekiroRequest request, SekiroResponse response)` 处理具体业务。
4. 如果 handler 内部抛异常，由框架层自动捕获。
5. 框架自动调用 `response.failed(Throwable)`，把异常和堆栈回传。
6. 不要求 Xposed 业务代码自己手写兜底 try/catch。

### 8.3 这部分对应的主要文件

- [SekiroLikeClient.java](/d:/r0rpc/java-client/src/com/r0rpc/client/SekiroLikeClient.java)
- [ActionHandler.java](/d:/r0rpc/java-client/src/com/r0rpc/sekiro/api/ActionHandler.java)
- [SekiroRequest.java](/d:/r0rpc/java-client/src/com/r0rpc/sekiro/api/SekiroRequest.java)
- [SekiroResponse.java](/d:/r0rpc/java-client/src/com/r0rpc/sekiro/api/SekiroResponse.java)
- [AutoBind.java](/d:/r0rpc/java-client/src/com/r0rpc/sekiro/api/databind/AutoBind.java)

### 8.4 Xposed demo 中的示例文件

- [MainHook.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/MainHook.java)
- [PingHandler.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/handler/PingHandler.java)
- [AppInfoHandler.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/handler/AppInfoHandler.java)
- [ToastHandler.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/handler/ToastHandler.java)
- [ForceFailHandler.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/handler/ForceFailHandler.java)

### 8.5 jar 输出

Java client 已经整理为可打包 jar 的形式，核心产物位置为：

- [r0rpc-xposed-client.jar](/d:/r0rpc/java-client/dist/r0rpc-xposed-client.jar)

Xposed demo 工程中也已经放入一份：

- [r0rpc-xposed-client.jar](/d:/r0rpc/xposed-demo/app/libs/r0rpc-xposed-client.jar)

## 9. 后端管理界面的目标与实现方向

你多次强调“后台不能太简陋”，所以最后管理界面不是只做一个简单列表，而是围绕运营和排障做了拆分。

主要方向包括：

1. 菜单化页面组织，避免所有内容堆在一个页面。
2. Group 管理页，支持分页、排序、在线状态、空 group 标识。
3. Group / Client 浏览页，尽量做成左侧树状目录式体验。
4. Client 详情页，支持查看最近请求与最近调用结果。
5. 请求记录页，支持多条件过滤。
6. 监控页，支持请求量、成功率等趋势图展示。
7. 请求过滤项中的 group / action / clientId 改为下拉可选。

前端页面由 Go 服务内嵌提供，核心位置：

- [index.html](/d:/r0rpc/internal/web/ui/index.html)
- [app.js](/d:/r0rpc/internal/web/ui/app.js)
- [app.css](/d:/r0rpc/internal/web/ui/app.css)
- [assets.go](/d:/r0rpc/internal/web/assets.go)

备注：由于静态资源是内嵌编译进 Go 二进制里的，所以前端改完后需要重新编译服务端，浏览器里看到的才会更新。

## 10. 配置方式与脱敏说明

### 10.1 统一配置文件

你要求不要继续依赖 `getenv`，因此现在项目统一读取文件配置。

核心配置加载文件：

- [config.go](/d:/r0rpc/internal/config/config.go)
- [r0rpc.conf](/d:/r0rpc/r0rpc.conf)

### 10.2 文档中的脱敏配置示例

以下是文档中可保留的脱敏示意，不展示真实值：

```conf
APP_NAME=r0rpc-demo
SERVER_ID=r0rpc-node-1
HTTP_ADDR=:9876
TIME_ZONE=Asia/Shanghai
JWT_SECRET=<redacted-jwt-secret>
REQUEST_TIMEOUT_SECONDS=25
RAW_RETENTION_DAYS=3
AGGREGATE_RETENTION_DAYS=30
DEVICE_OFFLINE_MINUTES=2

MYSQL_HOST=<redacted-mysql-host>
MYSQL_PORT=<redacted-mysql-port>
MYSQL_USER=<redacted-mysql-user>
MYSQL_PASSWORD=<redacted-mysql-password>
MYSQL_DB=r0rpc
MYSQL_PARAMS=charset=utf8mb4&parseTime=true&loc=Asia%2FShanghai&timeout=5s&readTimeout=30s&writeTimeout=30s

REDIS_ADDR=<redacted-redis-addr>
REDIS_PASSWORD=<redacted-redis-password>
REDIS_DB=<redacted-redis-db>

BOOTSTRAP_ADMIN_USERNAME=admin
BOOTSTRAP_ADMIN_PASSWORD=<redacted-bootstrap-password>
```

### 10.3 真实配置的处理原则

本次文档里不展示：

- 真实 MySQL 主机
- 真实 MySQL 端口
- 真实 MySQL 密码
- 真实 Redis 主机 / 地址
- 真实 Redis 密码
- 真实 JWT 密钥

但实际项目本地配置文件中已经写入并可运行。

## 11. 数据保留策略

你对这一点专门纠正过，所以这里单独明确写死：

1. 原始请求明细保留 3 天。
2. 聚合统计表保留 30 天。
3. 这样既能保留最近排障所需明细，又能保留一周和一个月的趋势分析能力。

这是本项目和“只保留 3 天一切数据”之间的重要差异点。

## 12. 时区修正

你明确指出过系统时间差了 8 小时，因此后来修正为：

- 统一使用 `Asia/Shanghai`
- 数据库连接参数中包含对应时区
- 应用层时间也统一按上海时区处理

相关核心代码位置：

- [config.go](/d:/r0rpc/internal/config/config.go)

## 13. Windows 与 Linux 启动方式的整理

项目中这部分你问得很多，因此这里专门归档。

### 13.1 Windows

你在 Windows 下测试时，最终已经确认：

- 服务可通过本地可执行文件启动。
- 实际访问地址是 `http://127.0.0.1:9876/`。
- 不是 `http://127.0.0.1:8080/` 之外的别的地址写法问题，而是要看配置文件里最终监听的端口。

相关运行辅助文件：

- [start-server.ps1](/d:/r0rpc/run/start-server.ps1)
- [windows-usage.md](/d:/r0rpc/run/windows-usage.md)
- [test-invoke.ps1](/d:/r0rpc/run/test-invoke.ps1)

### 13.2 Linux Docker

你后续准备部署到 Linux，并且希望“一键部署”。围绕这个目标，项目里整理了：

- [Dockerfile](/d:/r0rpc/Dockerfile)
- [docker-compose.yml](/d:/r0rpc/deploy/linux/docker-compose.yml)
- [deploy.sh](/d:/r0rpc/deploy/linux/deploy.sh)
- [stop.sh](/d:/r0rpc/deploy/linux/stop.sh)
- [start.sh](/d:/r0rpc/deploy/linux/start.sh)
- [README.md](/d:/r0rpc/deploy/linux/README.md)
- [\.env.example](/d:/r0rpc/deploy/linux/.env.example)

当时你还问过：

- `JWT_SECRET` 是什么，在哪里改。
- `BOOTSTRAP_ADMIN_PASSWORD` 默认是什么。
- Docker 是否只需要改 `.env` 文件就能部署。

这些都已经在部署文件和配置流程中对应处理。

## 14. Python demo 的定位

你问过“Python 怎么测试”。这里统一记录。

项目里的 Python demo 主要分成两类：

1. `invoke_demo.py`
   - 作用：模拟普通业务调用方。
   - 通过 HTTP 发起 RPC 调用。

2. `client_websocket.py`
   - 作用：模拟设备端。
   - 通过 WebSocket 常驻连接服务端，等待任务下发并返回结果。

3. `client_long_poll.py`
   - 作用：保留 long-poll 模式示例，用于理解和对比。
   - 当前主推荐链路不是它，而是 WebSocket。

相关文件：

- [invoke_demo.py](/d:/r0rpc/python-demo/invoke_demo.py)
- [client_websocket.py](/d:/r0rpc/python-demo/client_websocket.py)
- [client_long_poll.py](/d:/r0rpc/python-demo/client_long_poll.py)
- [README.md](/d:/r0rpc/python-demo/README.md)

## 15. 关键实现文件索引

为了便于以后继续改，这里汇总本次项目最重要的文件。

### 15.1 后端核心

- [main.go](/d:/r0rpc/cmd/server/main.go)
- [app.go](/d:/r0rpc/internal/app/app.go)
- [hub.go](/d:/r0rpc/internal/rpc/hub.go)
- [http.go](/d:/r0rpc/internal/web/http.go)
- [store.go](/d:/r0rpc/internal/store/store.go)
- [config.go](/d:/r0rpc/internal/config/config.go)
- [schema.sql](/d:/r0rpc/internal/store/schema.sql)

### 15.2 UI

- [index.html](/d:/r0rpc/internal/web/ui/index.html)
- [app.js](/d:/r0rpc/internal/web/ui/app.js)
- [app.css](/d:/r0rpc/internal/web/ui/app.css)
- [assets.go](/d:/r0rpc/internal/web/assets.go)

### 15.3 Java client / Sekiro 风格 API

- [SekiroLikeClient.java](/d:/r0rpc/java-client/src/com/r0rpc/client/SekiroLikeClient.java)
- [ActionHandler.java](/d:/r0rpc/java-client/src/com/r0rpc/sekiro/api/ActionHandler.java)
- [SekiroRequest.java](/d:/r0rpc/java-client/src/com/r0rpc/sekiro/api/SekiroRequest.java)
- [SekiroResponse.java](/d:/r0rpc/java-client/src/com/r0rpc/sekiro/api/SekiroResponse.java)
- [AutoBind.java](/d:/r0rpc/java-client/src/com/r0rpc/sekiro/api/databind/AutoBind.java)

### 15.4 Xposed demo

- [MainHook.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/MainHook.java)
- [PingHandler.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/handler/PingHandler.java)
- [AppInfoHandler.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/handler/AppInfoHandler.java)
- [ToastHandler.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/handler/ToastHandler.java)
- [ForceFailHandler.java](/d:/r0rpc/xposed-demo/app/src/main/java/com/r0rpc/xposed/handler/ForceFailHandler.java)

### 15.5 部署与运行

- [r0rpc.conf](/d:/r0rpc/r0rpc.conf)
- [Dockerfile](/d:/r0rpc/Dockerfile)
- [docker-compose.yml](/d:/r0rpc/deploy/linux/docker-compose.yml)
- [deploy.sh](/d:/r0rpc/deploy/linux/deploy.sh)
- [stop.sh](/d:/r0rpc/deploy/linux/stop.sh)

## 16. 本次对话中的典型问题与对应结论

这里把本次沟通里出现过的重要问答，按“问题 -> 结论”方式归档。

### 16.1 “为什么还要 client_long_poll”

结论：

- 它对应的是设备端长期等待任务的模型示例。
- 不是普通业务调用方。
- 普通业务调用方更像 `invoke_demo.py`。
- 最终推荐主方案是 `client_websocket.py`。

### 16.2 “invoke 是不是相当于普通 Python 去请求 RPC 数据”

结论：

- 是的。
- 这个理解是对的。

### 16.3 “多个设备的时候，是轮询制吗，要像原版 Sekiro 一样”

结论：

- 最终主设计不是 long-poll 轮询，而是 WebSocket 常连。
- 调度语义仍尽量保持成 Sekiro 那种“设备在线、服务端分派任务、设备返回结果”的模型。

### 16.4 “success 多次会不会影响返回”

结论：

- 不会让最终请求错位。
- 只认第一次生效结果，重复回包会被忽略。

### 16.5 “如果 XP 里报错了还有返回吗”

结论：

- 现在已按 Sekiro 风格补齐自动兜底。
- handler 内抛异常时，框架层自动转成 failed 返回。
- 不要求业务 handler 手写 `try/catch`。

### 16.6 “JWT_SECRET 是什么，在哪里改”

结论：

- 它是服务端签发和校验 JWT 登录令牌用的密钥。
- 当前统一在配置文件中维护。
- 文档里不展示真实值。

### 16.7 “Windows / Linux 的 MySQL 配置在哪改”

结论：

- 现在统一走配置文件，不再散落依赖环境变量。
- Windows 与 Linux 都以同一套配置项为准。

## 17. 当前项目产出物概览

到目前为止，这次 RPC 项目已经至少包含以下几类成果：

1. Go 后端服务。
2. 后台管理 UI。
3. MySQL 表结构与初始化逻辑。
4. Redis 对接。
5. Python invoke demo。
6. Python WebSocket 设备端 demo。
7. Python long-poll 示例。
8. Java client。
9. Sekiro 风格 API 封装。
10. Xposed demo 工程。
11. 可打包 jar。
12. Linux Docker 部署脚本。
13. 使用说明与补充文档。

## 18. 当前仍需留意的事项

虽然项目已经完成了主体搭建，但从工程角度仍有一些需要持续注意的地方：

1. 前端静态资源曾出现过编码问题，后续改动时要特别注意 UTF-8 编码。
2. 因为 UI 资源内嵌进 Go 二进制，修改前端后一定要重新编译后端。
3. 如果后续继续增强 Xposed 侧业务 handler，可以直接沿用当前 Sekiro 风格接口。
4. 如果后续想继续优化调度策略，可以在当前 WebSocket 通道基础上扩展更细的设备选路规则。
5. 如果后续需要更严格权限模型，还可以在账号体系上再分角色与细粒度权限。

## 19. 本次项目时间线整理

以下按时间顺序总结本次对话中最关键的推进过程。

1. 先讨论 RPC 本身和 Sekiro 的不足。
2. 你提出要参考 Sekiro，做 Go 语言 demo，并额外补齐后台登录、账号、监控、性能统计等能力。
3. 你给出已有 MySQL / Redis 配置，希望我直接负责把项目完整做出来。
4. 你要求除了后端，还要有 Java / Xposed 可用方案。
5. 你要求最终可以打 jar 给 Xposed 引用。
6. 你要求能部署到 Linux Docker，并希望一键部署。
7. 中途确认 `yxz` 是已有数据库，不能占用，改为新库 `r0rpc`。
8. 你要求真正的后台界面，而不是只有 API。
9. 你希望 Python demo 和 Xposed demo 都完整给出。
10. 你追问服务端、设备端、调用端三者关系，并确认 `invoke` 对应普通调用方。
11. 你不断推动后台 UI 优化，包括图表、筛选、树状浏览、分页排序、最近请求详情等。
12. 你指出在线状态显示不准，要求修正。
13. 你要求请求记录筛选项改成下拉可选，而不是手输。
14. 你从并发一致性角度追问“会不会请求和返回错位”。
15. 你专门追问 `success` 多次是否会影响返回结果。
16. 你要求比较 long-poll 与 WebSocket，并最终决定改成 WebSocket 方案。
17. 你要求不要继续依赖 `getenv`，统一改成文件配置。
18. 你指出原始明细与聚合统计保留策略不合理，重新定为 3 天和 30 天。
19. 你指出系统时区差 8 小时，需要修正。
20. 你要求 Java / Xposed 侧的编码体验尽量贴近 Sekiro 原版。
21. 你给出了一个 Sekiro 风格 handler 示例，要求按这种方式改造。
22. 你要求业务 handler 即使出错，也能自动返回 fail，而不是业务侧自己兜底。
23. 最后你要求把这次关于 RPC 的整套沟通和实现过程整理成本地 Markdown 记录，并隐藏真实数据库和 Redis 配置。
24. 当前这份文档就是基于这个要求重新整理的正式记录版本。

## 20. 本地记录文件位置

这份正式记录文件位于：

- [rpc-project-session-record-2026-03-30.md](/d:/r0rpc/docs/rpc-project-session-record-2026-03-30.md)

如果后面你还要，我可以继续在这份文档里追加：

- 更详细的接口说明
- 数据表说明
- 部署步骤清单
- Xposed 接入说明
- Python 调用说明
- WebSocket 时序图
- 与 Sekiro 原版差异对照表

## 21. 结论

这次项目已经不只是“仿 Sekiro 的一个 Go demo”，而是演进成了一套带管理后台、监控、统计、设备管理、Java / Xposed 接入、Python 调用和 Linux Docker 部署能力的可运营 RPC 平台。

而你在整个过程中最核心的要求，其实可以概括成三句话：

1. 调用体验尽量像 Sekiro。
2. 运维和管理能力要远强于 Sekiro。
3. 代码、配置、部署、示例都要落地，不能只停留在思路层。

这份文档就是围绕这三个目标，对本次完整实现过程做的留档。
