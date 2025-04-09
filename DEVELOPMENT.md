# Run services with `go run`

This section explains how to run some of the services using `go run` instead of the Docker setup to facilitate development.

## Running `sda-download` with `go run`

- Bring up all SDA services with the S3 backend and populate them with test data by running the following command in the root folder of the repository:

```sh
make integrationtest-sda-s3-run 
```

- Change to the folder `sda-download` and start the `sda-download` service using:

```sh
CONFIGFILE=dev_utils/config-notls_local.yaml go run cmd/main.go
```

- Check if `sda-download` works as expected using:

```sh
curl -o /dev/null -s -w "%{http_code}\n" http://localhost:18080/health
```

If successful, the curl command should output the HTTP code `200`.

You can further check the endpoint `/metadata/datasets` using:

```sh
curl -H "Authorization: Bearer $token" http://localhost:18080/metadata/datasets
```

where `token` can be obtained by logging to http://localhost:8801 with the dummy user.

If successful, the curl command should output a JSON body with an empty list and with HTTP code `200`:

```json
[]
```

## Running other SDA services with `go run`

Running any of the SDA services located in the `sda` subfolder requires that the service specific credentials and RabbitMQ configurations are set as ENVs. Here, we'll use `ingest` as an example.

- Bring up all SDA services with the S3 backend by running the following command in the root folder of the repository:

```sh
make sda-s3-up
```

- When the previous command is finished, bring down the `ingest` service using:

```sh
docker stop ingest
```

- Copy keys and other information from the shared folder of the container using:

```sh
docker cp verify:/shared /tmp/
```

This will copy all data from the container's `/shared` folder to `/tmp/shared` on your local machine, so that we have access to all the auto generated files that will be required.

- Change to the folder `sda` and start the `ingest` service using:

```sh
export BROKER_PASSWORD=ingest
export BROKER_USER=ingest
export BROKER_QUEUE=ingest
export BROKER_ROUTINGKEY=archived
export DB_PASSWORD=ingest
export DB_USER=ingest 
CONFIGFILE=config_local.yaml go run cmd/ingest/ingest.go
```

- Check if the `ingest` service works as expected by following these steps

    - Create a test file

        ```sh
        seq 10 > /tmp/t1.txt
        ```

    - Download the s3cmd config  file (`s3cmd-inbox.conf`) by logging to http://localhost:8801 with the admin user. After that update the config file by running

        ```sh
        sed -i '/host_/s/inbox:8000/localhost:18000/g' s3cmd-inbox.conf
        sed -i 's/use_https = True/use_https = False/g' s3cmd-inbox.conf
        ```

    - Upload the test file to S3inbox by `sda-cli`

        ```sh
        sda-cli -config s3cmd-inbox.conf upload -encrypt-with-key /tmp/shared/c4gh.pub.pem /tmp/t1.txt
        ```

    - Use `sda-admin` to check if the test file has been uploaded

        ```sh
        export API_HOST=http://localhost:8090
        export ACCESS_TOKEN=$(grep access_token s3cmd-inbox.conf  | awk '{print $3}')
        sda-admin file list -user testu@lifescience-ri.eu
        ```

        The file `testu_lifescience-ri.eu/t1.txt.c4gh` should have fileStatus `uploaded`


    - Register the Crypt4GH key

        ```sh
        curl -H "Authorization: Bearer $ACCESS_TOKEN" -H "Content-Type: application/json" -X POST -d '{"pubkey": "'"$( base64 -w0 /tmp/shared/c4gh.pub.pem)"'", "description": "pubkey"}' http://localhost:8090/c4gh-keys/add
        ```

    - Ingest the file test file

        ```sh
        sda-admin file ingest -filepath testu_lifescience-ri.eu/t1.txt.c4gh -user testu@lifescience-ri.eu
        ```

    - Verify that the test file has been ingested

        ```sh
        sda-admin file list -user testu@lifescience-ri.eu
        ```

        The file `testu_lifescience-ri.eu/t1.txt.c4gh` should have fileStatus `verified`
