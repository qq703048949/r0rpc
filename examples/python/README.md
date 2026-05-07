# Python Examples

This directory contains Python examples for invoking RPC and simulating a client.

## Files

- `invoke_demo.py`: direct invoke example using `username/password`
- `client_websocket.py`: websocket client demo
- `client_http_proxy.py`: websocket client that forwards jobs to a local HTTP service such as `127.0.0.1:30001`
- `client_long_poll.py`: compatibility wrapper to websocket entrypoint
- `invoke_http_proxy_demo.py`: invoke example for the local HTTP proxy client
- `requirements.txt`: Python dependencies

## Install

```bash
pip install -r requirements.txt
```

## Run invoke demo

```bash
python invoke_demo.py
```

## Local HTTP proxy flow

Start a client on the machine that can access `127.0.0.1:30001`:

```bash
python client_http_proxy.py
```

Then invoke through R0RPC:

```bash
python invoke_http_proxy_demo.py
```

Request body shape:

```json
{
  "username": "admin",
  "password": "123456",
  "timeoutSeconds": 30,
  "payload": {
    "method": "POST",
    "path": "/FinderURL2ID",
    "json": {
      "url": "https://channels.weixin.qq.com/web/pages/feed?eid=..."
    }
  }
}
```

Payload notes:

- `method`: optional, defaults to `GET`
- `path`: optional local path appended to `LOCAL_BASE_URL`, for example `/FinderURL2ID`
- `url`: optional full URL or relative URL; if present it overrides `path`
- `json`: optional JSON body sent with `requests`
- `data`: optional raw body or form body
- `headers`: optional request headers
- `params`: optional query parameters
- `localTimeoutSeconds`: optional timeout for the local `30001` request

If the local service returns JSON, the invoke response `data` field will be that JSON directly. If it returns non-JSON text, `data` will contain:

```json
{
  "text": "...",
  "contentType": "text/plain; charset=utf-8"
}
```

## Flat payload format

```json
{
  "username": "admin",
  "password": "123456",
  "timeoutSeconds": 20,
  "payload": {
    "encode_str": "xxx"
  }
}
```

Field notes:

- `timeoutSeconds`: optional timeout in seconds. `20` means wait up to 20 seconds for the device result.
- `clientId`: optional target device id. Omit it to let the server pick an online device in the group.
- `payload`: the real action parameters sent to the device. Keep it flat.
