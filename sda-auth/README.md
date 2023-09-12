# SDA authentication service

This service allows users to log in both via Elixir AAI or EGA.

## Configuration example for local testing

The following settings can be configured for deploying the service, either by using environment variables or a YAML file.

Parameter | Description | Defined value
--------- | ----------- | -------
`LOG_LEVEL` | Log level | `info`
`ELIXIR_ID` | Elixir authentication id | `XC56EL11xx`
`ELIXIR_SECRET` | Elixir authentication secret | `wHPVQaYXmdDHg`
`ELIXIR_PROVIDER` | Elixir issuer URL | `http://oidc:9090`
`ELIXIR_JWKPATH` | JWK endpoint where the public key of the Elixir issuer can be retrieved from for token validation | `/jwks`
`CEGA_AUTHURL` | CEGA server endpoint | `http://cega:8443/lega/v1/legas/users/`
`CEGA_ID` | CEGA server authentication id | `dummy`
`CEGA_SECRET` | CEGA server authentication secret | `dummy`
`CORS_ORIGINS` | Allowed Cross-Origin Resource Sharing (CORS) origins | `""`
`CORS_METHODS` | Allowed Cross-Origin Resource Sharing (CORS) methods | `""`
`CORS_CREDENTIALS` | If cookies, authorization headers, and TLS client certificates are allowed over CORS | `false`
`SERVER_CERT` | Certificate file path | `""`
`SERVER_KEY` | Private key file path | `""`
`S3INBOX` | S3 inbox host | `s3.example.com`
`JWTISSUER` | Issuer of JWT tokens | `http://auth:8080`
`JWTPRIVATEKEY` | Path to private key for signing the JWT token | `keys/sign-jwt.key`
`JWTSIGNATUREALG` | Algorithm used to sign the JWT token. ES256 (ECDSA) or RS256 (RSA) are supported | `RS256`
`RESIGNJWT` | Set to `false` to serve the raw OIDC JWT, i.e. without re-signing it | `""`
`C4GHPUBKEY` | c4gh key to be served to the info endpoint | `keys/c4gh_key.pub.pem`

## Running the development setup

Start the full stack by running docker-compose in the `dev-server` folder:

```bash
docker-compose up --build
```

The current setup also requires that `127.0.0.1  oidc` is added to `/etc/hosts`, so that routing works properly.

## Running with Cross-Origin Resource Sharing (CORS)

This service can be run as a backend only, and in the case where the frontend
is running somewhere else, CORS is needed.

Recommended cors settings for a given host are:

```txt
export CORS_ORIGINS="https://<frontend-url>"
export CORS_METHODS="GET,OPTIONS,POST"
export CORS_CREDENTIALS="true"
```

There is a minimal CORS login testing site at `http://localhost:8000` of the
dev-server.

## Building a Docker container

Using the provided Dockerfile, you may build a Docker image:

```bash
docker build -t neicnordic/sda-auth:mytag <path-to-Dockerfile-folder>
```
