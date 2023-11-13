#!/usr/bin/env python3.11
# -*- coding: utf-8 -*-

"""
Test server to act as CentralEGA endpoint for users

:author: Frédéric Haziza
:copyright: (c) 2018, EGA System Developers.
"""

import sys
import os
import logging
import json
from base64 import b64decode
import ssl

from aiohttp import web

logging.basicConfig(format="[%(levelname)-8s] (L:%(lineno)s) %(message)s")
LOG = logging.getLogger(__name__)
LOG.setLevel(logging.INFO)

filepath = None
instances = {}
store = None
usernames = {}


def fetch_user_info(identifier, query):
    LOG.info(f"Requesting User {identifier}")
    try:
        pos = usernames.get(identifier, None)
        return store[pos] if pos is not None else None
    except:
        raise web.HTTPBadRequest(text="Missing or wrong idType")


async def user(request):
    # Authenticate
    auth_header = request.headers.get("AUTHORIZATION")
    if not auth_header:
        raise web.HTTPUnauthorized(text=f"Protected access\n")
    _, token = auth_header.split(None, 1)  # Skipping the Basic keyword
    instance, passwd = b64decode(token).decode().split(":", 1)
    info = instances.get(instance)
    if info is None or info != passwd:
        raise web.HTTPUnauthorized(text=f"Protected access\n")

    # Reload users list
    load_users()

    # Find user
    user_info = fetch_user_info(request.match_info["identifier"], request.rel_url.query)
    if user_info is None:
        raise web.HTTPBadRequest(text=f"No info for that user\n")
    return web.json_response(user_info)


def main():

    if len(sys.argv) < 3:
        print("Usage: {sys.argv[0] <hostaddr> <port> <filepath>}", file=sys.stderr)
        sys.exit(2)

    host = sys.argv[1]
    port = sys.argv[2]

    global filepath
    filepath = sys.argv[3]

    server = web.Application()
    load_users()

    # Registering the routes
    server.router.add_get("/username/{identifier}", user, name="user")

    # SSL settings
    cacertfile = "/tls/ca.pem"
    certfile = "/tls/cega.pem"
    keyfile = "/tls/cega-key.pem"

    ssl_ctx = ssl.create_default_context(purpose=ssl.Purpose.SERVER_AUTH, cafile=cacertfile)
    ssl_ctx.check_hostname = False
    ssl_ctx.verify_mode = ssl.CERT_NONE

    ssl_ctx.load_cert_chain(certfile, keyfile=keyfile)

    # aaaand... cue music
    web.run_app(server, host=host, port=port, shutdown_timeout=0, ssl_context=ssl_ctx)


def load_users():
    # Initialization
    global filepath, instances, store, usernames, uids
    instances[os.environ[f"CEGA_USERS_USER"]] = os.environ[
        f"CEGA_USERS_PASSWORD"
    ]  #'legatest'  # Hard-coding legatest:legatest
    with open(filepath, "rt") as f:
        store = json.load(f)
    for i, d in enumerate(store):
        usernames[d["username"]] = i  # No KeyError, should be there


if __name__ == "__main__":
    main()
