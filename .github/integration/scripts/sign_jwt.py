from datetime import date, timedelta
from joserfc import jwt
from joserfc.jwk import RSAKey
from pathlib import Path
import sys

p = Path('/shared/keys/jwt.key')
raw = p.read_text()
key = RSAKey.import_key(raw)
iat = date.today() - timedelta(days=1)
exp = date.today() + timedelta(days=1)

header = {
    'alg': 'RS256',
    'kid': 'rsa1',
    'typ': 'JWT'
}

payload = {
    'aud': 'XC56EL11xx',
    'exp': exp.strftime('%s'),
    'iat': iat.strftime('%s'),
    'iss': 'http://localhost',
    'sub': sys.argv[1]
}

token = jwt.encode(header, payload, key)
print(token)