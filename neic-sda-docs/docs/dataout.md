Data Retrieval API
==================

> NOTE:
> We maintain two Data Out API solutions, for which REST APIs are the
> same.

SDA-DOA
-------

> NOTE:
> Source code repository is available at
> [https://github.com/neicnordic/sda-doa](https://github.com/neicnordic/sda-doa).

Configuration
-------------

Variable name                       | Default value                                                         |  Description
:-----------------------------------|:----------------------------------------------------------------------|:---------------------
`REST_ENABLED`                      | true                                                                  | Enables/disables REST endpoints of DOA
`SSL_ENABLED`                       | true                                                                  | Enables/disables TLS for DOA REST endpoints
`KEYSTORE_PATH`                     | /etc/ega/ssl/server.cert                                              | Path to server keystore file
`KEYSTORE_PASSWORD`                 |                                                                       | Password for the keystore
`OUTBOX_ENABLED`                    | true                                                                  | Enables/disables the outbox functionality
`OUTBOX_QUEUE`                      | exportRequests                                                        | MQ queue name for files/datasets export  requests
`OUTBOX_LOCATION`                   | /ega/outbox/p11-%s/files/                                             | Outbox location with placeholder for the username
`BROKER_HOST`                       | private-mq                                                            | Local RabbitMQ broker hostname
`BROKER_PORT`                       | 5671                                                                  | Local RabbitMQ broker port
`BROKER_VHOST`                      | /                                                                     | Local RabbitMQ broker virtual host
`BROKER_VALIDATE`                   | true                                                                  | Validate server MQ certificate or not
`DB_INSTANCE`                       | db                                                                    | Database hostname
`DB_PORT`                           | 5432                                                                  | Database port
`POSTGRES_DB`                       | lega                                                                  | Database name
`ROOT_CERT_PATH`                    | /etc/ega/ssl/CA.cert                                                  | Path to the CA file for database connectivity
`CERT_PATH`                         | /etc/ega/ssl/client.cert                                              | Path to the client cert for database connectivity
`CERT_KEY`                          | /etc/ega/ssl/client.key                                               | Path to the client key for database connectivity
`POSTGRES_USER`                     | lega_out                                                             | Database username
`POSTGRES_PASSWORD`                 |                                                                       | Database password
`S3_ENDPOINT`                       | vault                                                                 | S3 server hostname
`S3_PORT`                           | 443                                                                   | S3 server port
`S3_ACCESS_KEY`                     | minio                                                                 | S3 access key
`S3_SECRET_KEY`                     | miniostorage                                                          | S3 secret key
`S3_REGION`                         | us-west-1                                                             | S3 region
`S3_BUCKET`                         | lega                                                                  | S3 bucket to use
`S3_SECURE`                         | true                                                                  | true if S3 backend should be accessed over HTTPS
`S3_ROOT_CERT_PATH`                 | /etc/ssl/certs/ca-certificates.crt                                    | Path to the CA certs file for S3 connectivity
`ARCHIVE_PATH`                      | /                                                                     | Path to the filesystem-archive
`PASSPORT_PUBLIC_KEY_PATH`          | /etc/ega/jwt/passport.pem                                             | Path to the public key for passport JWT validation
`OPENID_CONFIGURATION_URL`          | <https://login.elixir-czech.org/oidc/.well-known/openid-configuration>| URL of the OpenID configuration endpoint
`VISA_PUBLIC_KEY_PATH`              | /etc/ega/jwt/visa.pem                                                 | Path to the public key for visas JWT validation
`CRYPT4GH_PRIVATE_KEY_PATH`         | /etc/ega/crypt4gh/key.pem                                             | Path to the Crypt4GH private key
`CRYPT4GH_PRIVATE_KEY_PASSWORD_PATH`| /etc/ega/crypt4gh/key.pass                                            | Path to the Crypt4GH private key passphrase
`LOGSTASH_HOST`                     |                                                                       | Hostname of the Logstash instance (if any)
`LOGSTASH_PORT`                     |                                                                       | Port of the Logstash instance (if any)

### Outbox functionality

> NOTE:
> Outbox can be disabled using `OUTBOX_ENABLED` environment variable.

Outbox in DOA is RabbitMQ-based listener that can be triggered by
incoming "export request". Template of such message:

```javascript
{
    "jwtToken": "...",         // mandatory: Elixir AAI token (see below)
    "datasetId": "...",        // optional: either datasetId, or fileId should be specified
    "fileId": "...",           // optional: either datasetId, or fileId should be specified
    "publicKey": "...",        // mandatory: Crypt4GH public key of the requester
    "startCoordinate": "...",  // optional
    "endCoordinate": "...",    // optional
}
```

Upon receival of such message, DOA acts exactly the same way as if this
information arrived via REST endpoint. The difference is that data is
not "returned" to the requester in a response, but is being dumped to
the outbox location (re-encrypted for the requester).

The reason for having this functionality is so-called "offline"
use-case, where DOA is running in the isolated environment (like TSD)
and can't expose REST API (but still can receive RabbitMQ messages).

Handling Permissions
--------------------

Data Out API can be run with connection to an AAI or without. In the
case connection to an AAI provider is not possible the
`PASSPORT_PUBLIC_KEY_PATH` and `CRYPT4GH_PRIVATE_KEY_PATH` need to be
set.

> NOTE:
> By default we use Elixir AAI as JWT for authentication
> `OPENID_CONFIGURATION_URL` is set to:
> <https://login.elixir-czech.org/oidc/.well-known/openid-configuration>

If connected to an AAI provider the current implementation is based on
[GA4GH
Passports](https://github.com/ga4gh/data-security/blob/master/AAI/AAIConnectProfile.md)

The AAI JWT payload should contain a GA4GH Passport claim in the scope:

```javascript
{
    "scope": "openid ga4gh_passport_v1",
    ...
}
```

The token is then intended to be delivered to the `/userinfo` endpoint
at AAI, which will respond with a list of assorted JWTs gathered from
providers that need to be parsed in order to find the relevant
information.

```javascript
{
    "ga4gh_passport_v1": [
        "JWT",
        "JWT",
        "JWT",
        ...
    ]
}
```

Each third party token (JWT, RFC 7519) consists of three parts separated
by dots, in the following manner: `header.payload.signature`. This
module processes the assorted tokens to extract the information they
carry and to validate that data. The process is carried out as such:

Dataset permissions are read from GA4GH RI claims of the type
"ControlledAccessGrants"

```javascript
{
    "ga4gh_visa_v1": {
        "type": "ControlledAccessGrants",
        "value": "https://www.ebi.ac.uk/ega/EGAD000000000001",
        "source": "https://ega-archive.org/dacs/EGAC00000000001",
        "by": "dac",
        "asserted": 1546300800,
        "expires": 1577836800
    }
}
```

SDA-download
------------

> NOTE:
> Source code repository is available at:
> [https://github.com/neicnordic/sda-download](https://github.com/neicnordic/sda-download)

Recommended provisioning method for production is:

-   on a `kubernetes cluster` using the [helm
    chart](https://github.com/neicnordic/sda-helm/).

`sda-download` focuses on enabling deployment of a stand-alone version
of SDA, with features such as:
 - trusted `JKU` and `ISS` pairs;
 - custom dataset names including DOI URLs;
 - etc.

REST API Endpoints
------------------

> NOTE:
> REST API can be disabled using `REST_ENABLED` environment variable.

API endpoints listed as OpenAPI specification is available:

```yaml
{% include "static/doa-api.yml" %}
```
