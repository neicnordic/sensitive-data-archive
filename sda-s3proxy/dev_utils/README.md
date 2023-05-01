# Dev environment setup recomendations

## Deploy a stack locally

To start the S3Proxy development environment locally with docker compose, run the following command from the directory `dev_utils`

```bash
docker compose run local
```

After that, you can use [s3cmd](https://s3tools.org/s3cmd) to manually interact with the s3 server with proxy by 

```bash
s3cmd -c proxyS3 put README.md s3://dummy ## Upload a file using the proxy
s3cmd -c proxyS3 ls s3://dummy ## List all files of the user using the proxy 
```

>Note that the content of the file `proxyS3` will be modified since the string `TOKEN` will be replaced by the actual token during the local deployment. Make sure not to commit this change.

If the above commands fail, you may also test if the interaction with the s3 server works without the proxy by
```bash
s3cmd -c directS3 ls s3 ## For access without using the proxy
```

## Trace requests to the minio server
This guide uses the
[minio client](https://docs.min.io/minio/baremetal/reference/minio-cli/minio-mc.html)
(mc) for testing.

Once the stack is deployed locally with docker compose, it's possible to trace all the requests that come to minio by first
putting the following in the hosts array of your `~/.mc/config.json` file:

```json
"proxydev": {
    "url": "http://localhost:9000",
    "accessKey": "ElixirID",
    "secretKey": "987654321",
    "api": "s3v4",
    "lookup": "auto"
}
```

and then run the following command in a terminal

```bash
mc admin trace -v proxydev
```

## Go proxy

Run the go proxy from the root directory

```bash
export SERVER_CONFFILE=dev_utils/config.yaml
go build .
./S3-Upload-Proxy
```


it's of course also possible to use the `mc` command from minio to access
through the proxy or directly but then you have to configure that in the
`~/.mc/config.json` file.
