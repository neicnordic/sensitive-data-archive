from datetime import date, timedelta
from joserfc import jwt
from joserfc.jwk import ECKey
from pathlib import Path

p = Path('/shared/keys/jwt.key')
raw = p.read_text()
key = ECKey.import_key(raw)
iat = date.today() - timedelta(days=1)
exp = date.today() + timedelta(days=1)

header = {
    'alg': 'ES256',
    'kid': key.thumbprint(),
    'typ': 'JWT'
}

payload = {
    'aud': 'XC56EL11xx',
    'exp': exp.strftime('%s'),
    'iat': iat.strftime('%s'),
    'iss': 'http://oidc',
    'sub': 'test@dummy.org'
}

token = jwt.encode(header, payload, key)
print(token)