# R0RPC

Linux-first RPC relay backend inspired by Sekiro RPC.

This repository is organized around production deployment on Linux.
The Go server remains at the repo root, while Java/Xposed/Python materials are grouped by role.

## Repository Layout

- `cmd/`: Go entrypoints
- `internal/`: Go application code
- `deploy/linux/`: one-click Linux deployment
- `clients/java/`: Java relay client SDK
- `clients/xposed-demo/`: Android/Xposed demo project
- `examples/python/`: Python invoke and client examples
- `docs/`: focused project docs
- `docs/archive/`: archived session records and raw transcripts

## Linux First

Primary deployment entry is:

```bash
cd deploy/linux
chmod +x quickstart.sh
./quickstart.sh
```

This flow is designed to:

1. install Docker and Docker Compose when missing
2. default to local Docker MySQL and local Docker Redis
3. allow switching to external MySQL and Redis by config
4. initialize database automatically
5. start R0RPC automatically

See [deploy/linux/README.md](r0rpc/deploy/linux/README.md).

## Config

Linux deployment uses:

- `deploy/linux/.env.docker`

Inside Docker, that file is mounted as `/app/r0rpc.conf` for the server process.
Root-level Windows-style local startup is no longer part of the project focus.

## Focused Docs

- architecture: [docs/architecture.md](r0rpc/docs/architecture.md)
- connection and transport: [docs/connection-and-transport.md](r0rpc/docs/connection-and-transport.md)
- Linux deployment: [deploy/linux/README.md](r0rpc/deploy/linux/README.md)
- Java client: [clients/java/README.md](r0rpc/clients/java/README.md)
- Xposed demo: [clients/xposed-demo/README.md](r0rpc/clients/xposed-demo/README.md)
- Python examples: [examples/python/README.md](r0rpc/examples/python/README.md)

- 中文总介绍: [docs/项目总览-中文.md](r0rpc/docs/项目总览-中文.md)

## 部署

'''
cd deploy/linux/
for f in *.sh; do sed -i 's/\r$//' "$f"; done
chmod +x *.sh
bash -x ./quickstart.sh
'''


## pc端转发
'''
client_websocket.py
client_http_proxy.py
这俩启动后相当于手机端连接服务器，然后对外提供了接口。
invoke_http_proxy_demo.py  相当于将参数请求通过云服务器 打到pc端接口，来实现建议版内网穿透
'''


## 额外说明
'''
1.http://159.75.100.225:9876/rpc/clientQueue?group=idlefish 拿到group为idlefish 下的所有设备

2.python demo位于examples/python/test_decrypt_demo.py

3.latencyMs为手机端从接收任务到返回数据的时间，并非整体链路时间。

4.遇到墙问题无法拉取下镜像时，参考https://github.com/DaoCloud/public-image-mirror


sudo mkdir -p /etc/docker/
#添加到 /etc/docker/daemon.json

{
  "registry-mirrors": [
    "https://docker.m.daocloud.io"
  ]
}

sudo systemctl daemon-reload && sudo systemctl restart docker

'''