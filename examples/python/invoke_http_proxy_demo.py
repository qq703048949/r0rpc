import json
import os

import requests

BASE_URL = os.getenv('R0RPC_BASE_URL', 'http://159.75.100.225:9876/')
ADMIN_USER = os.getenv('R0RPC_USERNAME', 'admin')
ADMIN_PASS = os.getenv('R0RPC_PASSWORD', '123456')
GROUP = os.getenv('R0RPC_GROUP', 'demo-group')
ACTION = os.getenv('R0RPC_ACTION', 'http_proxy')
CLIENT_ID = os.getenv('R0RPC_TARGET_CLIENT_ID', '')


def invoke(payload: dict, timeout_seconds: int = 30):
    body = {
        'username': ADMIN_USER,
        'password': ADMIN_PASS,
        'timeoutSeconds': timeout_seconds,
        'payload': payload,
    }
    if CLIENT_ID:
        body['clientId'] = CLIENT_ID

    response = requests.post(
        f'{BASE_URL}/rpc/invoke/{GROUP}/{ACTION}',
        json=body,
        timeout=timeout_seconds + 10,
    )
    response.raise_for_status()
    return response.json()


def main():
    result = invoke({
        'method': 'POST',
        'path': '/FinderURL2ID',
        'json': {
            'url': 'https://channels.weixin.qq.com/web/pages/feed?eid=export/UzFfBgAAxMmABAprTBK9jczT4DCsIvbBDx8sLTcd3RfG_ELsm_0fA_KhqQ',
        },
    })
    print(json.dumps(result, indent=2, ensure_ascii=False))


if __name__ == '__main__':
    main()
