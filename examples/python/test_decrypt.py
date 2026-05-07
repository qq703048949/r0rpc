import requests

BASE_URL = 'http://159.75.100.225:9876/'
ADMIN_USER = 'admin'
ADMIN_PASS = '123456'


def login():
    response = requests.post(
        f'{BASE_URL}/api/auth/login',
        json={'username': ADMIN_USER,
              'password': ADMIN_PASS},
        timeout=10,
    )
    response.raise_for_status()
    return response.json()['token']


def query_seller_id(token, seller_id):
    headers = {'Authorization': f'Bearer {token}'}
    _json = {
        'timeoutSeconds': 1,
        'payload': {
            "encode_str": seller_id
        }
    }

    url = f'http://159.75.100.225:9876/rpc/invoke/idlefish/decrypt'

    resp = requests.post(url, headers=headers, json=_json, timeout=5)
    print(resp.text)


for i in range(100):
    token = login()
    seller_id = 'v7eNwdELBmc1hOkagpP6NQ=='
    query_seller_id(token, seller_id)
