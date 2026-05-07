# RPC 项目问答最终记录（2026-03-31）

> 说明
>
> - 本文档只保留“你的提问”和“对应的最终结论答案”。
> - 不记录中间调试过程、来回试错、临时方案。
> - 适合作为项目交接、需求说明和对外讲解材料。
> - 涉及真实数据库与 Redis 敏感配置的地方，统一做脱敏处理。

## 1. 这个项目最终是什么

### 问

参考 Sekiro RPC 工具，给我写一份 Go 语言的 demo，要实现原来的功能，并增加：

1. 后端能登录，能增加账号，控制账号是否能访问。
2. 能监控不同的 `group` 的访问情况，并能查询最近数据，可以按 `group / action / client` 过滤，数据本身只保留 3 天即可。
3. 能监控性能指标，能监控每个设备一星期的请求数之类。

### 答

最终交付的是一整套可运行的 RPC 系统，而不是单一 demo，包含：

- Go 后端服务
- 内嵌前端管理后台
- WebSocket RPC 通道
- Python demo 客户端
- Java client jar
- Xposed demo
- Linux Docker 部署脚本

已经实现的核心能力：

- 后台账号登录、创建账号、启停账号、控制是否允许 RPC
- `group / action / clientId` 多维过滤
- 请求明细、最近调用结果、趋势图表、设备监控
- 原始明细保留 3 天
- 聚合统计保留 30 天
- 每个 `group + action + clientId` 只保留最近 100 条原始记录
- 每设备一周请求统计
- Xposed / Java / Python 三端示例
- Docker 一键部署脚本

## 2. 数据库和 Redis 怎么处理的

### 问

我给你数据库配置，你给我新建 db、新建表。
后面又补充：`yxz` 是我已有的库，你另外搞个库，改成 `r0rpc`。

### 答

最终使用的是单独的新库：

```text
r0rpc
```

不是复用你已有的 `yxz`。

系统会创建并维护自己的表，主要包括：

- `users`
- `devices`
- `rpc_requests`
- `device_daily_metrics`
- `rpc_daily_metrics`

数据库和 Redis 的真实连接信息在项目里按你的要求使用了固定配置来源，文档里不再明文记录密码。

## 3. RPC 本身有什么缺点

### 问

首先 RPC 有什么缺点？

### 答

最终结论是：RPC 能解决跨进程、跨设备调用问题，但天然有这些成本：

- 依赖网络，不像本地函数调用那样稳定
- 超时、断线、重试、重复回包都需要专门处理
- 请求和返回必须严格做关联，否则高并发时会错位
- 错误链路复杂，客户端异常、网络异常、服务端异常都要分别兜底
- 监控很重要，否则出问题时很难排查

所以这套系统最后重点补了：

- `requestId` 级别的结果匹配
- WebSocket 长连接
- 自动重连
- 心跳超时判定
- 结果去重
- 后台监控和请求记录

## 4. 最终为什么选 WebSocket，不用 long-poll 做主方案

### 问

WebSocket 版本 client，和 long-poll 版本比，优缺点是什么？
后面又明确要求：改成 WebSocket，我要极致性能。

### 答

最终生产主方案是：

```text
WebSocket
```

保留 long-poll 兼容接口，但不作为主路径。

最终结论：

#### WebSocket 优点

- 长连接，设备在线时不需要频繁轮询
- 延迟更低，服务端可以立即下发任务
- 大量设备场景下更省 CPU 和带宽
- 心跳、任务、结果走同一条连接，链路更清晰

#### WebSocket 缺点

- 连接生命周期更复杂
- 需要处理半开连接、断线重连、心跳超时
- 反向代理需要支持 WebSocket Upgrade

#### Long-poll 优点

- 实现简单
- 调试简单
- 纯 HTTP，兼容性高

#### Long-poll 缺点

- 空闲时大量重复请求
- 高设备数时浪费资源
- 实时性比不上热连接的 WebSocket

最终因为你明确要“极致性能”，所以主通道已经改成 WebSocket。

## 5. 现在客户端是怎么连服务端的

### 问

我是给你服务器 id 和端口去连接吗？
又问：为什么构造里是 `http://`，不是 `wss://`？

### 答

客户端连接服务端时，最终只需要：

- `baseUrl`
- `username`
- `password`
- `clientId`
- `group`
- 可选 `platform`

其中：

- `baseUrl` 是服务地址，不是 `serverId`
- `serverId` 只是服务端节点标识，用于后台展示和多节点排查
- 客户端先通过 HTTP 登录拿到 `token` 和 `wsUrl`
- 然后再根据服务端返回的 `wsUrl` 建 WebSocket

所以构造里传 `127.0.0.1:9876` 或 `101.x.x.x:9876` 是没问题的，客户端会自动补全为：

```text
http://127.0.0.1:9876
```

之后真正的数据通道会转到：

```text
ws://.../api/client/ws?token=...
```

如果未来用 HTTPS 反代，则会自然变成：

```text
wss://...
```

## 6. 登录校验会不会拖慢性能

### 问

现在是不是每次都去 HTTP 校验登录？会不会导致性能变慢？

### 答

最终结论：不会每次 RPC 都重新登录。

流程是：

1. 客户端先 `POST /api/client/login`
2. 登录成功后拿到 `token`
3. 用 `token` 建立 WebSocket
4. 后续任务和结果都在 WebSocket 里完成

也就是说：

- 登录只发生在连接建立阶段
- 不是每个 RPC 都重新做一次 HTTP 登录
- 日常 RPC 性能瓶颈不在这里

## 7. 为了防止大量设备同时连接，有没有做退避

### 问

为了应对短时间大量设备同时连接，是不是还要考虑雪崩？指数重连？

### 答

已经加了：

- 指数退避重连
- 最大重连间隔限制
- 随机抖动（jitter）
- 心跳也加了抖动，避免大量设备同一秒齐刷刷发心跳

这能降低大规模断网恢复时的瞬时冲击。

## 8. 多设备场景下，是否像 Sekiro 一样调度

### 问

多个设备的时候是轮询制吗？我需要和 Sekiro 原版一样。

### 答

最终服务端在 `group` 维度上实现了类似 Sekiro 的轮转调度思路：

- 同一个 `group` 下有多个在线 `client`
- 不指定 `clientId` 时，会从该 `group` 的在线设备里选取目标设备
- 指定 `clientId` 时，会优先投递给指定设备
- 结果按 `requestId + clientId` 严格匹配

所以在行为上已经接近你要的“像 Sekiro 一样”的设备调度方式。

## 9. 并发时请求和返回会不会错位

### 问

Sekiro 的 `SekiroResponse` 是和请求绑定的。你这个大量并发的时候能保证请求和返回不错位吗？

### 答

最终答案是：

```text
能保证，不会错位。
```

实现原则是：

- 每个请求生成唯一 `requestId`
- 服务端保存 `requestId -> waiter/clientId` 的等待关系
- 客户端结果回传时必须携带同一个 `requestId`
- 如果回包的 `clientId` 不匹配，会直接拒绝
- 超时后的迟到结果会被识别并忽略
- 重复结果也会被去重，只接受第一次有效提交

这就是最终的并发安全保证。

## 10. 如果客户端代码里抛异常，是否也会返回 fail

### 问

像 Sekiro 一样，如果 XP 里报错了，我也要能收到 fail，而且不想每个 handler 都自己写一堆 try。

### 答

最终已经做成：

- 业务 handler 可以像 Sekiro 一样直接写正常逻辑
- jar 底层会统一兜底 `try/catch`
- 一旦 handler 抛异常，会自动返回失败结果
- 失败结果里会带错误信息，便于 Python 或后台排查

也就是说，使用体验已经靠近：

```java
response.success(...)
response.fail(...)
```

这种风格，但你不需要在每个业务类里手写全套兜底。

## 11. Java / Xposed 端最终是什么风格

### 问

我想像 Sekiro 一样，直接写 handler，继承类后在类里写具体实现。

### 答

最终 Java / Xposed 端已经整理成这种风格：

- 一个 `RelayClient`
- 多个 `RelayHandler`
- 每个 handler 对应一个 `action`
- 业务代码只关心参数和返回

示意用法：

```java
new RelayClient(
    "127.0.0.1:9876",
    "client_demo",
    "Client@123456",
    "device-001",
    "demo-group"
)
    .registerHandler(new PingHandler())
    .registerHandler(new AppInfoHandler(lpparam))
    .start();
```

## 12. 为什么不需要自己手写 Thread

### 问

为什么还要我在 XP 代码里自己 new Thread？我要像 Sekiro 一样直接用。

### 答

最终已经处理成：

- `RelayClient.start()` 内部自己创建后台线程
- 外部不需要自己包 `Thread`
- Xposed 侧调用更接近一行式启动

也就是说，你现在直接：

```java
client.start();
```

就可以，不需要自己额外手写线程包装。

## 13. 默认错误日志怎么处理

### 问

`.onError(...)` 能不能默认就有？而且不要用 Xposed 的 log，直接用 `android.util.Log`。

### 答

最终已经按你的要求改成：

- 默认错误输出走 `android.util.Log`
- 不依赖 `XposedBridge.log`
- 即使是魔改 XP，也不依赖原版 Xposed 的日志实现

这样 RPC 层就更“纯”，不带明显的 Xposed 特征点。

## 14. jar 名字里不要带 `xposed`

### 问

jar 包名里不要带 `xposed` 字样，我后面可能会魔改 XP，不想增加检测点。

### 答

最终 Java 客户端 jar 产物使用的是纯 RPC 命名，不再以 Xposed 为中心命名。

当前 jar 产物路径：

```text
D:\r0rpc\java-client\dist\r0rpc-relay-client.jar
```

## 15. `clientId`、`deviceName`、`platform` 最终怎么定

### 问

XP 里 `clientId` 不是已经包含了 `android.os.Build.MODEL` 吗？有了 `clientId`，`deviceName` 实际可以省略，对吗？
后面又问：`platform` 做什么用？XP 里是默认值吗？

### 答

最终结论：

#### 15.1 `deviceName`

```text
deviceName 已经整体移除。
```

它已经从这些地方去掉：

- 后端接口
- 数据模型
- 前端展示
- Python demo
- Java client
- Xposed demo
- 文档
- SQL 模板

现在设备标识统一用：

```text
clientId
```

#### 15.2 `platform`

`platform` 不是设备唯一标识，而是“运行环境标签”，主要用于：

- 后台区分来源
- 排查问题
- 后续做按来源过滤统计

例如：

- `android`
- `python`
- `java`
- `websocket`

#### 15.3 XP 默认值

XP 当前默认使用：

```text
android
```

如果你以后想改，也可以自定义成自己的值。

## 16. 后端管理界面最终做到了什么

### 问

我需要真正的后端管理界面，而且不要太挤，要分页面、菜单栏、图表化、可搜索、可分页、可排序。

### 答

最终后台已经不是简陋页面，而是完整的管理控制台，包含这些页面：

- 总览
- Group 管理
- Group / Client 浏览
- 请求记录
- 设备监控
- 账户管理
- 手动调用

已经落地的界面能力：

- 图表展示请求量、成功率、延迟
- 7 / 15 / 30 天趋势切换
- `group / action / clientId` 下拉过滤
- Group 管理分页和排序
- Group / Client 左侧树状浏览
- 点击某个 client 查看最近请求和最近结果
- 请求记录支持“查看详情”弹框
- 弹框支持点击外部关闭
- 多处布局做过压缩与解挤处理
- 中文化替换了英文说明

## 17. Group / Client 浏览和请求记录最终怎么分工

### 问

Group / Client 浏览、最近请求 / 最近调用结果、请求记录，这几个界面是不是重复？

### 答

最终分工是：

- `Group / Client 浏览`：偏“设备视角”，用于看某个 group 下有哪些 client，以及某个 client 最近情况
- `请求记录`：偏“调用视角”，用于统一查看最近请求、结果、参数、错误、状态码

并且做了取舍：

- 一些重复入口已经去掉，避免页面太挤
- 详情类信息尽量归到请求记录或弹框里

## 18. 请求记录和筛选最后是什么规则

### 问

后台请求记录要能按 `group / action / clientId` 过滤，而且最好是下拉框。每个 `group + client + action` 只保留最后 100 条。

### 答

最终已经做成：

#### 18.1 筛选方式

- `group` 下拉框
- `action` 下拉框
- `clientId` 下拉框
- 状态筛选
- 分页

#### 18.2 保留规则

- 原始明细全局保留 3 天
- 同时再做 scope 限制：每个 `group + action + clientId` 仅保留最近 100 条

#### 18.3 聚合统计

- 聚合统计保留 30 天
- 所以 7 天趋势、15 天趋势、30 天趋势都可以正常看

## 19. 索引有没有做好

### 问

该做的索引做好。

### 答

已经补了关键索引，重点覆盖：

- `devices(group_name, last_seen_at)`
- `devices(group_name, status, last_seen_at)`
- `rpc_requests(group_name, client_id, created_at)`
- `rpc_requests(action_name, created_at)`
- `rpc_requests(client_id, created_at)`
- 聚合统计相关日期索引

这样是为了保证：

- 后台过滤不拖慢
- 趋势统计还能接受
- 设备列表、请求记录、Group 汇总查询更稳

## 20. 心跳与离线判定最终怎么定的

### 问

离线多久算离线？我希望 20 秒内心跳不回应就算断了，学习 Sekiro。

### 答

最终离线判定采用：

```text
20 秒
```

也就是：

- 客户端通过 WebSocket 保持心跳
- 如果 20 秒内没有有效心跳/活动，服务端会把设备视为离线
- 后台在线状态就按这个逻辑刷新

同时还做了：

- 心跳写 MySQL 节流
- 不是每次心跳都重写数据库
- 内存实时态优先用于展示在线状态

这样既保证“离线判断够快”，也避免 MySQL 被心跳打爆。

## 21. 原始明细和聚合数据保留多久

### 问

我要统计 7 天，但原始请求你只保留 3 天不对。最终要求：

1. 原始明细保留 3 天即可。
2. 聚合统计表保留 1 个月。

### 答

最终就是按这个落地的：

- `rpc_requests` 原始请求明细：保留 3 天
- `device_daily_metrics`、`rpc_daily_metrics` 聚合统计：保留 30 天

所以：

- 你可以看一周、半月、30 天趋势
- 同时原始明细不会无限膨胀

## 22. 时区问题怎么修的

### 问

系统里的时区不对，差了 8 小时。

### 答

最终后端和数据库会话时区已经按东八区处理，避免后台看到的时间比本地慢 8 小时。

项目当前运行环境基准是：

```text
Asia/Shanghai
```

## 23. 手动调用最终返回什么

### 问

手动调用失败时，下面结果区域不会变；我还需要状态码、抓包结果、错误结果都能展示。

### 答

最终手动调用结果区域已经做成：

- 成功会显示结果
- 失败也会刷新结果框
- 会显示 `httpStatus / httpCode`
- 会保留 `requestId`
- 会显示 `clientId`
- 会显示原始请求体 `requestPayload`
- 成功结果统一放在：

```json
"data": { ... }
```

不再把最终业务返回放在旧的 `payload` 命名里

## 24. 返回里为什么要带 `clientId`

### 问

我需要返回里带上 `clientId`，后续排查问题方便。

### 答

最终已经补上。

无论是：

- 手动调用结果
- 失败回包
- 客户端队列查询

都会尽量把命中的 `clientId` 带出来，方便定位到底是哪台设备处理的。

## 25. `payload` 为什么改成 `data`

### 问

我希望最终返回写到单独的 `data` 字段里，不想混在 `payload`。

### 答

最终外部调用端看到的是：

- 请求内容：`requestPayload`
- 业务结果：`data`
- 错误信息：`error`

这样更直观，Python 端也更好接。

## 26. 客户端到服务端的大数据压缩怎么做

### 问

XP 到服务器端如果是大文件、大 JSON，希望先压缩，服务端再解压后返回给 Python 调用端。

### 答

最终结论是：

- 客户端到服务端这段支持做压缩优化
- 服务端收到后解压，再按正常 JSON 结果返回给调用端
- Python 端不需要额外感知压缩细节

你的核心要求是：

```text
只优化手机端 -> 服务端的带宽，Python 端看到的仍然是普通结果。
```

最终实现就是朝这个方向设计的。

## 27. 心跳去 MySQL 节流、RPC 结果异步落库是否做了

### 问

先给我做：

- 心跳去 MySQL 节流
- RPC 结果异步落库

### 答

已经做了。

最终效果：

- 在线态主要靠内存实时状态 + 节流刷库
- 心跳不会每次都打数据库
- RPC 结果与指标更新改成异步持久化
- 降低高频请求下的数据库压力

## 28. `rpc/clientQueue` 最终是什么

### 问

Sekiro 有：

```text
http://sekiro.iinti.cn/business/clientQueue?group=test
```

我也需要有这个接口，但路径不要 `business/clientQueue`，改成：

```text
rpc/clientQueue
```

### 答

最终兼容接口已经是：

```http
GET /rpc/clientQueue?group=demo-group
```

默认返回该 `group` 当前在线的 `client` 列表。

返回字段包括：

```json
{
  "group": "demo-group",
  "count": 2,
  "clientIds": ["client-a", "client-b"],
  "items": [
    {
      "clientId": "client-a",
      "group": "demo-group",
      "platform": "android",
      "status": "online",
      "lastSeenAt": "2026-03-31T20:00:00+08:00",
      "lastIp": "127.0.0.1"
    }
  ]
}
```

## 29. 账号系统最终有哪些能力

### 问

后端能登录、能增加账号、控制账号是否能访问。

### 答

最终账号系统已经包含：

- 管理员登录
- 创建账号
- 启用/禁用账号
- 重置密码
- 控制是否允许发起 RPC

后台区分：

- `admin`
- `client`

其中：

- `admin` 用于后台管理和发起 RPC
- `client` 用于设备端登录

## 30. Python demo 最终怎么用

### 问

Python 怎么测试？

### 答

Python demo 最终是一个 WebSocket client，作用是模拟设备端。

核心用法：

```python
client = WebSocketClient(
    base_url='http://127.0.0.1:9876',
    username='client_demo',
    password='Client@123456',
    client_id='python-device-001',
    group='demo-group',
)
```

然后注册 action：

```python
@client.register('ping')
def handle_ping(payload):
    return {
        'ok': True,
        'message': 'pong from python websocket',
    }
```

再运行：

```python
client.serve_forever()
```

它本质上是“设备端”，不是“调用端”。

## 31. Java client 最终怎么用

### 问

我要 Java 项目，还要打 jar 包给 Xposed 引用。

### 答

最终已经提供 Java 客户端源码和可打包 jar。

基础用法：

```java
RelayClient client = new RelayClient(
    "127.0.0.1:9876",
    "client_demo",
    "Client@123456",
    "device-001",
    "demo-group"
);
```

构建脚本：

```powershell
powershell -ExecutionPolicy Bypass -File .\java-client\build.ps1
```

最终 jar 产物：

```text
D:\r0rpc\java-client\dist\r0rpc-relay-client.jar
```

## 32. Xposed demo 最终怎么用

### 问

还要给我 Xposed 项目 demo。

### 答

最终 Xposed demo 已经提供，入口思路是：

- 在目标进程加载时启动 `RelayClient`
- 注册多个 `RelayHandler`
- 长期监听服务端任务

当前示意：

```java
new RelayClient(
    "YOUR_SERVER_IP:9876",
    "client_demo",
    "Client@123456",
    clientId,
    "demo-group"
)
    .registerHandler(new PingHandler())
    .registerHandler(new AppInfoHandler(lpparam))
    .registerHandler(new ToastHandler(context))
    .registerHandler(new ForceFailHandler())
    .start();
```

## 33. Windows 下最终怎么启动

### 问

Windows 下怎么启动，我要先测通。

### 答

最终 Windows 启动方式就是直接运行服务端可执行文件：

```powershell
.\bin\r0rpc-server.exe
```

如果服务端监听的是：

```text
:9876
```

那浏览器访问：

```text
http://127.0.0.1:9876/
```

如果当时配置还是 `8080`，那就访问对应端口。

## 34. Linux Docker 最终怎么部署

### 问

我要部署到 Linux，Docker 要一键部署。

### 答

最终项目已经提供 Linux 部署目录和脚本，思路是：

```bash
cd /your/path/r0rpc/deploy/linux
cp .env.example .env.docker
chmod +x deploy.sh stop.sh
./deploy.sh
```

最终会以 Docker 方式拉起：

- R0RPC 服务
- MySQL
- Redis
- 反向代理（按部署方案决定）

你后续只需要维护部署配置文件即可。

## 35. `JWT_SECRET` 是什么，在哪改

### 问

`JWT_SECRET` 是什么？在哪改？

### 答

最终结论：

- `JWT_SECRET` 是服务端签发和校验 JWT token 的密钥
- 用于后台管理员 token 和客户端登录 token
- 必须服务端统一配置
- 改了之后，旧 token 会全部失效

你要求“直接给我随机值”，这一要求已经体现在项目配置体系里。

## 36. `BOOTSTRAP_ADMIN_PASSWORD` 是什么

### 问

`BOOTSTRAP_ADMIN_PASSWORD` 默认密码是什么？

### 答

它是系统第一次启动时用于创建初始管理员账号的默认密码。

也就是说，第一次 bootstrap 时会自动创建：

- 管理员账号
- 管理员默认密码

后续你可以在后台改密码。

## 37. 为什么需要 Nginx

### 问

为什么需要 Nginx？

### 答

最终结论：Nginx 不是 RPC 本身必须，但部署上很有价值。

主要作用：

- 转发 WebSocket Upgrade
- 做 HTTPS / WSS 终结
- 统一对外入口
- 未来多实例时便于负载均衡

如果是本机开发调试，可以不加；如果是线上部署，建议保留。

## 38. “手机断网后会不会自动重连”

### 问

手机断网后，手机是否会自动重连？

### 答

会。

当前客户端已经具备：

- 断线重连
- 指数退避
- 抖动
- 长连接恢复

所以网络恢复后会自动尝试重新连上。

## 39. 结果多次 success 会不会影响返回

### 问

RPC 过程中可能会 `success` 多次，这不会对我的返回产生影响吧？

### 答

最终不会影响主调用结果。

系统只会接受：

- 第一次有效结果

后续同 `requestId` 的重复结果会被识别为：

- 重复结果
- 或迟到结果

并被忽略，不会覆盖已经返回给调用端的结果。

## 40. 后台乱码问题最终处理结论

### 问

多次反馈后台乱码、右上角乱码、登录提示乱码、中文显示乱掉。

### 答

最终处理原则是：

- 前端资源统一按 `UTF-8` 输出
- 页面响应头带 `charset=utf-8`
- 对 Windows 本地写文件引起的编码问题做过专项修复
- 重要文档在需要时转成 `UTF-8 BOM`，避免 Windows 打开乱码

当前核心前端文件已经按这个方向修正。

## 41. 最终重要文件和产物在哪

### 问

我要知道项目主要位置。

### 答

当前核心文件：

```text
D:\r0rpc\internal\app\app.go
D:\r0rpc\internal\web\http.go
D:\r0rpc\internal\web\client_ws.go
D:\r0rpc\internal\rpc\hub.go
D:\r0rpc\internal\store\store.go
D:\r0rpc\internal\store\schema.sql
D:\r0rpc\internal\web\ui\app.js
D:\r0rpc\python-demo\client_websocket.py
D:\r0rpc\java-client\src\com\r0rpc\client\RelayClient.java
D:\r0rpc\xposed-demo\app\src\main\java\com\r0rpc\xposed\MainHook.java
```

当前构建产物：

```text
D:\r0rpc\bin\r0rpc-server.exe
D:\r0rpc\java-client\dist\r0rpc-relay-client.jar
```

## 42. 当前最终结论

### 问

到目前为止，这套能不能跑起来？

### 答

当前最终结论是：

```text
已经能跑起来。
```

并且已经具备你这一轮最核心的目标：

- 后端可登录、可管账号、可控 RPC 权限
- 后台可看 Group / Client / 请求 / 趋势 / 设备监控
- WebSocket 主通道可用
- Xposed / Java / Python 三端示例齐全
- 结果匹配、防错位、异常兜底、重复结果忽略都已落实
- `rpc/clientQueue` 已提供
- `deviceName` 已全部移除，统一以 `clientId` 为准

## 43. 补充：现在客户端最简连接参数

最终最简连接参数如下：

```json
{
  "baseUrl": "127.0.0.1:9876",
  "username": "client_demo",
  "password": "Client@123456",
  "clientId": "brand-model-pid",
  "group": "demo-group",
  "platform": "android"
}
```

其中：

- `platform` 可选
- `deviceName` 不再需要

## 44. 补充：当前典型调用返回格式

```json
{
  "requestId": "619d4438e237aeb21cdf74d9",
  "group": "demo-group",
  "action": "ping",
  "clientId": "brand-model-1001",
  "requestPayload": {
    "msg": "hello from console"
  },
  "status": "success",
  "httpCode": 200,
  "data": {
    "ok": true,
    "message": "pong"
  },
  "latencyMs": 12,
  "error": ""
}
```

如果失败，则会保留：

```json
{
  "requestId": "...",
  "group": "demo-group",
  "action": "ping",
  "clientId": "",
  "requestPayload": {
    "msg": "hello from console"
  },
  "error": "no online client in group"
}
```

---

本文档到这里为止，已经按“问答最终结论”的方式覆盖了本轮 RPC 项目的核心诉求、最终设计和当前落地结果。