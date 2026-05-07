import json
import threading
import time
from dataclasses import dataclass
from typing import Any, Callable, Dict, Optional
from urllib.parse import quote

import requests
import websocket


@dataclass
class HandlerResult:
    payload: Any
    status: str = 'success'
    http_code: int = 200
    error: str = ''


class WebSocketClient:
    def __init__(self, base_url: str, username: str, password: str, client_id: str, group: str):
        self.base_url = base_url.rstrip('/')
        self.username = username
        self.password = password
        self.client_id = client_id
        self.group = group
        self.token = ''
        self.ws_url = ''
        self.handlers: Dict[str, Callable[[Dict[str, Any]], Any]] = {}
        self._running = False
        self._socket: Optional[websocket.WebSocket] = None

    def register(
        self,
        action: str,
        handler: Optional[Callable[[Dict[str, Any]], Any]] = None,
    ):
        if handler is None:
            def decorator(func: Callable[[Dict[str, Any]], Any]):
                self.handlers[action] = func
                return func
            return decorator

        self.handlers[action] = handler
        return handler

    def login(self):
        response = requests.post(
            f'{self.base_url}/api/client/login',
            json={
                'username': self.username,
                'password': self.password,
                'clientId': self.client_id,
                'group': self.group,
                'platform': 'python-websocket',
            },
            timeout=15,
        )
        response.raise_for_status()
        body = response.json()
        self.token = body['token']
        self.ws_url = body.get('wsUrl') or self._build_ws_url()

    def _build_ws_url(self) -> str:
        if self.base_url.startswith('https://'):
            base = 'wss://' + self.base_url[len('https://'):]
        else:
            base = 'ws://' + self.base_url[len('http://'):]
        return f'{base}/api/client/ws?token={quote(self.token)}'

    def _heartbeat_loop(self, sock: websocket.WebSocket):
        while self._running and self._socket is sock:
            try:
                sock.send(json.dumps({'type': 'heartbeat'}))
            except Exception:
                return
            time.sleep(5)

    def _handle_job(self, sock: websocket.WebSocket, job: Dict[str, Any]):
        action = job['action']
        handler = self.handlers.get(action)
        started = time.time()
        try:
            if handler is None:
                raise RuntimeError(f'no handler for action: {action}')
            payload = handler(job.get('payload') or {})
            if isinstance(payload, HandlerResult):
                result = {
                    'requestId': job['requestId'],
                    'status': payload.status or 'success',
                    'httpCode': payload.http_code or 200,
                    'payload': payload.payload if payload.payload is not None else {},
                    'error': payload.error or '',
                    'latencyMs': int((time.time() - started) * 1000),
                }
            else:
                result = {
                    'requestId': job['requestId'],
                    'status': 'success',
                    'httpCode': 200,
                    'payload': payload if payload is not None else {},
                    'error': '',
                    'latencyMs': int((time.time() - started) * 1000),
                }
        except Exception as exc:
            result = {
                'requestId': job['requestId'],
                'status': 'error',
                'httpCode': 500,
                'payload': {},
                'error': str(exc),
                'latencyMs': int((time.time() - started) * 1000),
            }
        sock.send(json.dumps({'type': 'result', 'result': result}, ensure_ascii=False))

    def serve_forever(self):
        if not self.token:
            self.login()
        self._running = True
        while self._running:
            sock = None
            try:
                sock = websocket.create_connection(self.ws_url or self._build_ws_url(), timeout=30)
                self._socket = sock
                heartbeat = threading.Thread(target=self._heartbeat_loop, args=(sock,), daemon=True)
                heartbeat.start()
                while self._running and self._socket is sock:
                    raw = sock.recv()
                    if not raw:
                        break
                    message = json.loads(raw)
                    if message.get('type') == 'job' and message.get('job'):
                        self._handle_job(sock, message['job'])
            except Exception:
                time.sleep(2)
                if not self._running:
                    break
                if not self.token:
                    self.login()
            finally:
                self._socket = None
                if sock is not None:
                    try:
                        sock.close()
                    except Exception:
                        pass

    def stop(self):
        self._running = False
        if self._socket is not None:
            try:
                self._socket.close()
            except Exception:
                pass


def main():
    client = WebSocketClient(
        base_url='http://159.75.100.225:9876/',
        username='admin',
        password='123456',
        client_id='python-device-001',
        group='demo-group',
    )

    @client.register('ping')
    def handle_ping(payload: Dict[str, Any]):
        return {
            'ok': True,
            'message': 'pong from python websocket',
        }

    client.serve_forever()


if __name__ == '__main__':
    main()
