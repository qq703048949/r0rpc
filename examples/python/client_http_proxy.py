import json
import os
import sys
from pathlib import Path
from typing import Any, Dict
from urllib.parse import urljoin

import requests

# Make sibling example imports work no matter where the script is launched from.
CURRENT_DIR = Path(__file__).resolve().parent
if str(CURRENT_DIR) not in sys.path:
    sys.path.insert(0, str(CURRENT_DIR))

from client_websocket import HandlerResult, WebSocketClient


BASE_URL = os.getenv('R0RPC_BASE_URL', 'http://159.75.100.225:9876/')
USERNAME = os.getenv('R0RPC_USERNAME', 'admin')
PASSWORD = os.getenv('R0RPC_PASSWORD', '123456')
CLIENT_ID = os.getenv('R0RPC_CLIENT_ID', 'python-http-proxy-001')
GROUP = os.getenv('R0RPC_GROUP', 'demo-group')
ACTION = os.getenv('R0RPC_ACTION', 'http_proxy')
LOCAL_BASE_URL = os.getenv('LOCAL_BASE_URL', 'http://127.0.0.1:30001')
DEFAULT_LOCAL_TIMEOUT = float(os.getenv('LOCAL_TIMEOUT_SECONDS', '20'))


def build_target_url(payload: Dict[str, Any]) -> str:
    raw_url = str(payload.get('url') or '').strip()
    if raw_url:
        if raw_url.startswith('http://') or raw_url.startswith('https://'):
            return raw_url
        return urljoin(LOCAL_BASE_URL.rstrip('/') + '/', raw_url.lstrip('/'))

    path = str(payload.get('path') or '').strip()
    if not path:
        return LOCAL_BASE_URL.rstrip('/') + '/'
    return urljoin(LOCAL_BASE_URL.rstrip('/') + '/', path.lstrip('/'))


def build_request_kwargs(payload: Dict[str, Any]) -> Dict[str, Any]:
    kwargs: Dict[str, Any] = {
        'headers': payload.get('headers') or {},
        'params': payload.get('params') or {},
        'timeout': float(payload.get('localTimeoutSeconds') or DEFAULT_LOCAL_TIMEOUT),
    }
    if 'json' in payload:
        kwargs['json'] = payload.get('json')
    elif 'data' in payload:
        body = payload.get('data')
        if isinstance(body, (dict, list)):
            kwargs['data'] = json.dumps(body, ensure_ascii=False)
            kwargs['headers'] = {**kwargs['headers']}
            kwargs['headers'].setdefault('Content-Type', 'application/json')
        else:
            kwargs['data'] = body
    return kwargs


def decode_response_body(response: requests.Response) -> Any:
    if not response.content:
        return {}
    try:
        return response.json()
    except ValueError:
        return {
            'text': response.text,
            'contentType': response.headers.get('Content-Type', ''),
        }


def forward_to_local_service(payload: Dict[str, Any]) -> HandlerResult:
    method = str(payload.get('method') or 'GET').upper()
    url = build_target_url(payload)
    response = requests.request(method, url, **build_request_kwargs(payload))
    body = decode_response_body(response)
    if response.ok:
        return HandlerResult(payload=body, http_code=response.status_code)
    return HandlerResult(
        payload=body,
        status='error',
        http_code=response.status_code,
        error=f'local service returned HTTP {response.status_code}',
    )


def main():
    client = WebSocketClient(
        base_url=BASE_URL,
        username=USERNAME,
        password=PASSWORD,
        client_id=CLIENT_ID,
        group=GROUP,
    )

    @client.register(ACTION)
    def handle_http_proxy(payload: Dict[str, Any]):
        return forward_to_local_service(payload)

    print(
        f'http proxy client started: action={ACTION} group={GROUP} '
        f'local_base={LOCAL_BASE_URL} client_id={CLIENT_ID}'
    )
    client.serve_forever()


if __name__ == '__main__':
    main()
