#!/usr/bin/env python3
"""Minimal SigV4 S3 client (stdlib only) for test setup, replacing `mc`.

Usage: s3.py METHOD URL [FILE]   (FILE "-" reads the body from stdin)
Credentials come from S3_ACCESS_KEY / S3_SECRET_KEY, region us-east-1.
TLS certificates are not verified; this is only meant for test stacks.
"""
import datetime, hashlib, hmac, os, ssl, sys, urllib.error, urllib.parse, urllib.request

method, url = sys.argv[1].upper(), sys.argv[2]
body = b""
if len(sys.argv) > 3:
    body = sys.stdin.buffer.read() if sys.argv[3] == "-" else open(sys.argv[3], "rb").read()

access, secret, region = os.environ["S3_ACCESS_KEY"], os.environ["S3_SECRET_KEY"], "us-east-1"
u = urllib.parse.urlsplit(url)
# Canonical path and query per SigV4: each part URI-encoded exactly once,
# query pairs sorted, and the request is sent with the same encoding.
path = urllib.parse.quote(urllib.parse.unquote(u.path or "/"), safe="/~")
query = "&".join(f"{urllib.parse.quote(k, safe='~')}={urllib.parse.quote(v, safe='~')}"
                 for k, v in sorted(urllib.parse.parse_qsl(u.query, keep_blank_values=True)))
url = urllib.parse.urlunsplit((u.scheme, u.netloc, path, query, ""))
now = datetime.datetime.now(datetime.timezone.utc)
amzdate, day = now.strftime("%Y%m%dT%H%M%SZ"), now.strftime("%Y%m%d")
payload = hashlib.sha256(body).hexdigest()
headers = {"host": u.netloc, "x-amz-content-sha256": payload, "x-amz-date": amzdate}
signed = ";".join(sorted(headers))
canonical = "\n".join([method, path, query,
                       "".join(f"{k}:{headers[k]}\n" for k in sorted(headers)), signed, payload])
scope = f"{day}/{region}/s3/aws4_request"
to_sign = "\n".join(["AWS4-HMAC-SHA256", amzdate, scope, hashlib.sha256(canonical.encode()).hexdigest()])
key = ("AWS4" + secret).encode()
for part in (day, region, "s3", "aws4_request"):
    key = hmac.new(key, part.encode(), hashlib.sha256).digest()
sig = hmac.new(key, to_sign.encode(), hashlib.sha256).hexdigest()
headers["authorization"] = f"AWS4-HMAC-SHA256 Credential={access}/{scope}, SignedHeaders={signed}, Signature={sig}"

req = urllib.request.Request(url, data=body if method in ("PUT", "POST") else None, method=method, headers=headers)
try:
    with urllib.request.urlopen(req, context=ssl._create_unverified_context()) as r:
        sys.stdout.buffer.write(r.read())
except urllib.error.HTTPError as e:
    sys.exit(f"{method} {url}: {e.code} {e.read().decode(errors='replace')}")
