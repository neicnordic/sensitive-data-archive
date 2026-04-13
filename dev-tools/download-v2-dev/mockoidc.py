"""Mock OIDC server for download service local development.

Provides JWT/JWKS validation, userinfo with GA4GH visas, and a /tokens
endpoint for easy token retrieval (matching the v1 developer experience).

Uses the same RSA key as make_download_credentials.sh so tokens are
accepted by the download service.
"""

from aiohttp import web
from authlib.jose import jwt, RSAKey
import json
import base64
import time

# RSA private key - same as in make_download_credentials.sh
PRIVATE_KEY_PEM = """-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDhuZjxPmOGUIW1
LhxzKfxkN+1aTbvI5w+AptqT33X+bWuzfjvhEodiNz0bBfQgJJpQ3TZ8J1IZpM2F
Tnzox+FGxKPe5T9Mgngzd4N6eByWVPXoNMk7IdmBXMdPZBFSyjMW4ba1MELCpiKV
05de4J5opRDwmHmyMqYJxBk78e3iiYYixVk+j1Ku+yFl4d2R29y2+O9PlZegJloe
8FGnKIGZApS/8t9iyCkXg8WbjSPzgYCTQKxn/E4lcGdTrAt/McKrWmAuppcr+rpP
+BInm3l5Zu/QiRSZcMb5O460ojP9eKnaUlDpGZv9CY5j4x4lq8vjU2kK77YXBO8I
2oxse5a5AgMBAAECggEABbwSX6anHqVzECxQurhJWj51gELTT4JXSXxztygJNmKP
RushGFHBMMSYf9RB5IMpjH5iQPs6wb4HHqjk0YEqfwLF6wbF+eqipSQXKghdKZCV
AsY8io0MmpXB1omDSygp7h3j52yHdayE2muav+VTAPOYn5QwG0/gGgVqYrR9x7CM
iTuyOIuGNO4Wlly4/5RhLtSo0pal9AgBvX4crtVEwN8tPgqPVo9w71bSROt9EVNI
3cZiFFrrapYiifckIGiPGQYQUd5ej9Mq/77Fa0fv0pk0ONQV8HwstQ5HY2WwJWsn
mccF9plVTzem7N/vo+T+hFRPUO9TZUao91mMV8iV5QKBgQD1nZbQW3NHdol0fXA8
nw5JRkTLZx1zcZ5l36WVPkwCjJOyXQ2vWHm4lz7F81Rr8dQnMKLWMDKjrBT9Dbfs
xYK2bYxENS1W/n+0jOIaX/792DY9tfX7vvHU9yGSdoJE5os6DGCHYInOD0xnRmnl
3vS7gKv8miDwDzFsbjtDg6WfSwKBgQDrRLkmmfZCMcmLA02YSrErAlUseuyad7lY
HEJApXKfn262iHELlQa2zOBZpJGXIcHsNf1XGpMeU5pH+ILKE4Y5qbclq+AzFCcZ
nBFUfDeawmWdV5FJqNDd1L8Mb8aE+6q0Y5rNb3RL7A2ypH2ZeYKSGpHz3C7Rn5KW
voWAXRWriwKBgQCH4bxK3x0ivxiCgtcyIojDzwVGRnDLqmMIVzeDHqjsjBs2BTcJ
9/e3QK1w1BKzeWF2oPilaJrLY+tkqE9FxWtwQ6DjJ0xDIZ9DIuH/13X5t8EiWOWS
devSdzpyje+58JW78pcArk7u2hXZ2OHDU5qvlRsRL6/jP3SHWWCeFFnviwKBgGov
M02r0YygwfEfBYeFtp7Nx7lypZU2Eg4levWIdsp6f9KclEEA+u3IXD25XAiVMNw2
pegJU3stioWPMSCZXUxrQAEdqOwE3XzehqfWBJaxxIEWQ7m2Gsb0PWIUlMnyeGJA
Tl8IPboCiVAmk5WQVREyMsuYhf0Qg23MAZ8k5CHvAoGBAJm55NQZVKAEDGd4a21q
TDcRddtPwwL2oP3qa0gbGk4YFRUCrX99hIejOTvQW1xf6vGxTd7E1QizvFse4yRz
ZRKyXIc7DCcdzOnpMrSd1+aXwZtRHLSw0EDS6PWeJZdjJYHxl2YpAmMdURdcGTrH
b6b/6vhU90+xL14CX7Awofp/
-----END PRIVATE KEY-----"""

# Load RSA key
KEY = RSAKey.import_key(PRIVATE_KEY_PEM)
PUBLIC_JWK = KEY.as_dict(is_private=False)
PUBLIC_JWK["kid"] = "rsa1"
PUBLIC_JWK["use"] = "sig"
PUBLIC_JWK["alg"] = "RS256"
PRIVATE_JWK = dict(KEY)


def generate_token(sub: str) -> str:
    """Generate a signed JWT for a user."""
    header = {
        "alg": "RS256",
        "typ": "JWT",
        "kid": "rsa1",
    }
    now = int(time.time())
    payload = {
        "iss": "http://mockauth:8000",
        "sub": sub,
        "aud": "XC56EL11xx",
        "iat": now,
        "exp": now + 86400,  # 24 hours
        "jti": f"dev-token-{sub}",
    }
    return jwt.encode(header, payload, PRIVATE_JWK).decode("utf-8")


def generate_visa(dataset: str, sub: str) -> str:
    """Generate a signed visa JWT for a dataset."""
    header = {
        "alg": "RS256",
        "typ": "JWT",
        "kid": "rsa1",
        "jku": "http://mockauth:8000/jwks",
    }
    payload = {
        "iss": "https://demo.example",
        "sub": sub,
        "ga4gh_visa_v1": {
            "type": "ControlledAccessGrants",
            "value": dataset,
            "source": "https://doi.example/no_org",
            "by": "self",
            "asserted": 1568699331,
        },
        "iat": 1571144438,
        "exp": 9999999999,
        "jti": f"visa-{dataset}",
    }
    return jwt.encode(header, payload, PRIVATE_JWK).decode("utf-8")


# Pre-generate visas for test datasets
VISAS = {}


def get_visas_for_user(sub: str) -> list:
    """Get visas for a user, generating them if needed."""
    if sub not in VISAS:
        VISAS[sub] = [
            generate_visa("EGAD00000000001", sub),
        ]
    return VISAS[sub]


async def well_known(request: web.Request) -> web.Response:
    """Return OIDC discovery document."""
    print("[mockoidc] GET /.well-known/openid-configuration")
    config = {
        "issuer": "http://mockauth:8000",
        "authorization_endpoint": "http://mockauth:8000/authorize",
        "token_endpoint": "http://mockauth:8000/token",
        "userinfo_endpoint": "http://mockauth:8000/userinfo",
        "jwks_uri": "http://mockauth:8000/jwks",
        "response_types_supported": ["code", "token"],
        "subject_types_supported": ["public"],
        "id_token_signing_alg_values_supported": ["RS256"],
        "scopes_supported": ["openid", "ga4gh_passport_v1"],
        "claims_supported": ["sub", "iss", "aud", "exp", "iat", "ga4gh_passport_v1"],
    }
    return web.json_response(config)


async def jwks(request: web.Request) -> web.Response:
    """Return JSON Web Key Set."""
    print("[mockoidc] GET /jwks")
    return web.json_response({"keys": [PUBLIC_JWK]})


async def tokens(request: web.Request) -> web.Response:
    """Serve pre-generated tokens for development.

    Returns a JSON array with one token for integration_test@example.org
    which has access to all seeded test datasets.
    """
    print("[mockoidc] GET /tokens — generating dev token")
    data = [
        generate_token("integration_test@example.org"),
    ]
    return web.json_response(data)


async def userinfo(request: web.Request) -> web.Response:
    """Return user info with GA4GH visas."""
    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        return web.json_response(
            {"error": "invalid_token", "error_description": "Missing bearer token"},
            status=401,
        )

    print("[mockoidc] GET /userinfo")
    token = auth_header.split(" ")[1]
    try:
        parts = token.split(".")
        if len(parts) != 3:
            raise ValueError("Not a JWT")

        payload_b64 = parts[1].replace("-", "+").replace("_", "/")
        padding = 4 - len(payload_b64) % 4
        if padding != 4:
            payload_b64 += "=" * padding

        payload = json.loads(base64.b64decode(payload_b64))
        sub = payload.get("sub", "unknown@example.org")
    except Exception:
        sub = token

    visas = get_visas_for_user(sub)
    response = {
        "sub": sub,
        "ga4gh_passport_v1": visas,
    }
    return web.json_response(response)


def init() -> web.Application:
    """Initialize the web application."""
    app = web.Application()
    app.router.add_get("/.well-known/openid-configuration", well_known)
    app.router.add_get("/jwks", jwks)
    app.router.add_get("/tokens", tokens)
    app.router.add_get("/userinfo", userinfo)
    return app


if __name__ == "__main__":
    print("[mockoidc] Starting mock OIDC server on port 8000")
    print("[mockoidc] GET /tokens  - fetch dev tokens")
    print("[mockoidc] GET /jwks    - JSON Web Key Set")
    print("[mockoidc] GET /userinfo - user info with visas")
    web.run_app(init(), port=8000)
