import json
import requests

BASE_URL = 'http://159.75.100.225:9876/'
ADMIN_USER = 'admin'
ADMIN_PASS = '123456'


def invoke(group: str, action: str, payload: dict, client_id: str = ''):
    body = {
        'username': ADMIN_USER,
        'password': ADMIN_PASS,
        'timeoutSeconds': 20,
        'payload': payload,
    }
    if client_id:
        body['clientId'] = client_id

    response = requests.post(
        f'{BASE_URL}/rpc/invoke/{group}/{action}',
        json=body,
        timeout=30,
    )
    response.raise_for_status()
    return response.json()


def main():
    result = invoke('demo-group', 'ping', {'msg': 'hello from python invoke'})
    print('is_ok =', result.get('is_ok'))
    print('status =', result.get('status', ''))
    print('clientId =', result.get('clientId', ''))
    print('requestPayload =', json.dumps(result.get('requestPayload', {}), ensure_ascii=False))
    print('data =', json.dumps(result.get('data', {}), ensure_ascii=False))
    print(json.dumps(result, ensure_ascii=False, indent=2))

    print()
    print('username/password invoke example:')
    print(json.dumps({
        'username': ADMIN_USER,
        'password': ADMIN_PASS,
        'timeoutSeconds': 20,
        'payload': {
            'encode_str': 'your-value'
        }
    }, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
