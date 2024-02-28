# SDA authentication service

This service allows users to log in both via LS-AAI (OIDC) or EGA (NSS).

After successsful authentication the users will be able to get `accesstoken` and download the `S3 config file` needed in order to be able to upload files to the [S3Inbox service](../s3inbox/s3inbox.md).

## Choosing provider login

The `auth` allows for two different type of login providers: `EGA` and `LS_AAI` (OIDC). It is possible, however, to run the service using only one of the providers.

In order to remove the `EGA` option, remove the `CEGA_ID` and `CEGA_SECRET` options from the configuration, while for removing the `LS-AAI` option, remove the `OIDC_ID` and `OIDC_SECRET` variables.

## Configuration example for local testing

The following settings can be configured for deploying the service, either by using environment variables or a YAML file.

| Parameter               | Description                                                                          | Defined value                           |
| ----------------------- | ------------------------------------------------------------------------------------ | --------------------------------------- |
| `AUTH_CEGA_AUTHURL`     | CEGA server endpoint                                                                 | `http://cega:8443/lega/v1/legas/users/` |
| `AUTH_CEGA_ID`          | CEGA server authentication id                                                        | `dummy`                                 |
| `AUTH_CEGA_SECRET`      | CEGA server authentication secret                                                    | `dummy`                                 |
| `AUTH_CORS_CREDENTIALS` | If cookies, authorization headers, and TLS client certificates are allowed over CORS | `false`                                 |
| `AUTH_CORS_METHODS`     | Allowed Cross-Origin Resource Sharing (CORS) methods                                 | `""`                                    |
| `AUTH_CORS_ORIGINS`     | Allowed Cross-Origin Resource Sharing (CORS) origins                                 | `""`                                    |
| `AUTH_JWT_ISSUER`       | Issuer of JWT tokens                                                                 | `http://auth:8080`                      |
| `AUTH_JWT_PRIVATEKEY`   | Path to private key for signing the JWT token                                        | `keys/sign-jwt.key`                     |
| `AUTH_JWT_SIGNATUREALG` | Algorithm used to sign the JWT token. ES256 (ECDSA) or RS256 (RSA) are supported     | `ES256`                                 |
| `AUTH_RESIGNJWT`        | Set to `false` to serve the raw OIDC JWT, i.e. without re-signing it                 | `""`                                    |
| `AUTH_S3INBOX`          | S3 inbox host                                                                        | `http://s3.example.com`                 |
| `LOG_LEVEL`             | Log level                                                                            | `info`                                  |
| `OIDC_ID`               | OIDC authentication id                                                               | `XC56EL11xx`                            |
| `OIDC_SECRET`           | OIDC authentication secret                                                           | `wHPVQaYXmdDHg`                         |
| `OIDC_PROVIDER`         | OIDC issuer URL                                                                      | `http://oidc:8080`                      |
| `OIDC_JWKPATH`          | JWK endpoint where the public key can be retrieved for token validation              | `/jwks`                                 |
| `SERVER_CERT`           | Certificate file path                                                                | `""`                                    |
| `SERVER_KEY`            | Private key file path                                                                | `""`                                    |

## Running with Cross-Origin Resource Sharing (CORS)

This service can be run as a backend only, and in the case where the frontend is running somewhere else, CORS is needed.

Recommended CORS settings for a given host are:

```txt
AUTH_CORS_ORIGINS="https://<frontend-url>"
AUTH_CORS_METHODS="GET,OPTIONS,POST"
AUTH_CORS_CREDENTIALS="true"
```

A minimal CORS login (for testing purposes) can look like this:

```html
<!DOCTYPE html>
<html lang="en">

<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>CORS login test page</title>
</head>

<body>
    <a href="http://localhost:8080/oidc?redirect_uri=http://localhost:8000">Log in</a>
    <br>
    <a href="http://localhost:8000/">Reset</a>

    <div id="download"></div>
    <pre id="result"></pre>
</body>

<script>
    const $ = document.querySelector.bind(document)
    const authURL = `http://localhost:8080/oidc`

    const params = new URLSearchParams(document.location.href.split('?')[1])
    if (params.has("code") && params.has("state")) {
        const url = `${authURL}/cors_login?${params.toString()}`
        fetch(url, { credentials: 'include' })
            .then(data => data.json())
            .then(r => {
                $("#result").innerHTML = JSON.stringify(r, null, 2)
                let element = document.createElement('a')
                let s3conf_data = ""
                for (const key in r["S3Conf"]) {
                    s3conf_data += `${key} = ${r["S3Conf"][key]}\n`
                }

                element.setAttribute('href', 'data:text/plain;charset=utf-8,', encodeURIComponent(s3conf_data))
                element.setAttribute('download', 's3cmd.conf')
                element.innerHTML = "download s3conf"

                document.getElementById("download").appendChild(element)
            })
    }
</script>

</html>
```
