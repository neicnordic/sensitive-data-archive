## Run services with `go run`

This section explains how to run some of the services using `go run` instead of the Docker setup to facilitate development.

### Running `sda-download` with `go run`
1. Bring up all SDA services with the S3 backend and populate them with test data by running the following command in the root folder of the repository:
```sh
make integrationtest-sda-s3-run 
```

2. Change to the folder `sda-download` and start the `sda-download` service using:
```sh
CONFIGFILE=dev_utils/config-notls_local.yaml go run cmd/main.go
```
> If the host `oidc` cannot be accessed on the local machine, update your `/etc/hosts` file by adding the following line to ensure the service can resolve `oidc` locally:
> ```
> 127.0.0.1 oidc
> ```

3. Check if `sda-download` works as expected using:
```sh
curl -o /dev/null -s -w "%{http_code}\n" http://localhost:18080/health
```
If successful, the curl command should output the HTTP code `200`.

You can further check the endpoint `/metadata/datasets` using:
```sh
token=$(curl -s -k http://localhost:8080/tokens | jq -r '.[0]') 
curl -H "Authorization: Bearer $token" http://localhost:18080/metadata/datasets
```
If successful, the curl command should output a JSON body containing:
```json
["EGAD74900000101"]
```


### Running other SDA services with `go run`
Running other SDA services located in the `sda` subfolder, such as `ingest` or `verify`, differs slightly from running the `sda-download` service. Here, we'll use `ingest` as an example.

1. Bring up all SDA services with the S3 backend and populate them with test data by running the following command in the root folder of the repository:
```sh
make integrationtest-sda-s3-run 
```

2. Bring down the `ingest` service using:
```sh
PR_NUMBER=$(/bin/date +%F)  docker compose -f .github/integration/sda-s3-integration.yml stop ingest  
```

3. Change to the folder `sda`, then copy keys and other information from the shared folder of the container using:
```sh
docker cp verify:/shared /tmp/
```
This will copy all data from the container's `/shared` folder to `/tmp/shared` on your local machine.

4. Start the `ingest` service using:
```sh
export BROKER_PASSWORD=ingest
export BROKER_USER=ingest
export BROKER_QUEUE=ingest
export BROKER_ROUTINGKEY=archived
export DB_PASSWORD=ingest
export DB_USER=ingest 
CONFIGFILE=config_local.yaml go run cmd/ingest/ingest.go
```

5. Check if the `ingest` service works as expected by following these steps
```sh
# create a test file
seq 10 > /tmp/t1.txt

# update the s3cmd config file
sed -i '/host_/s/s3inbox:8000/localhost:18000/g' /tmp/shared/s3cfg

# upload /tmp/t1.txt to s3inbox by sda-cli
sda-cli -config /tmp/shared/s3cfg upload -encrypt-with-key /tmp/shared/c4gh.pub.pem /tmp/t1.txt

# use sda-admin to check if t1.txt has been uploaded
export API_HOST=http://localhost:8090
export ACCESS_TOKEN=$(curl -s -k http://localhost:8080/tokens | jq -r '.[0]') 
sda-admin file list -user test@dummy.org # file t1.txt should have fileStatus 'uploaded'

# use sda-admin to ingest the file t1.txt
sda-admin file ingest -filepath test_dummy.org/t1.txt.c4gh -user test@dummy.org  

# verify that t1.txt has been uploaded using sda-admin 
sda-admin file list -user test@dummy.org # file t1.txt should have fileStatus 'verified'
```
