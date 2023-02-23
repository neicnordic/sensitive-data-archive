#!/bin/bash

if [ "$STORAGETYPE" = s3notls ] || [ "$STORAGETYPE" = s3notlsheader ]; then
    exit 0
fi

cd dev_utils || exit 1

chmod 600 certs/client-key.pem

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT * from local_ega_ebi.file_dataset"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT * from local_ega_ebi.filedataset ORDER BY id DESC"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT id, status, stable_id, archive_path FROM local_ega.files ORDER BY id DESC"

# "Disable" old files to not confuse later tests.
docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -A -c "UPDATE local_ega.files SET status='DISABLED' where status='READY';"

count=1

for file in dummy_data.c4gh largefile.c4gh; do
	curl --cacert certs/ca.pem -u test:test 'https://localhost:15672/api/queues/test/verified' | jq -r '.["messages_ready"]'

	# Give some time to avoid confounders in logs
	sleep 10

	now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

	md5sum=$(md5sum "$file" | cut -d' ' -f 1)
	sha256sum=$(sha256sum "$file" | cut -d' ' -f 1)

	C4GH_PASSPHRASE=$(grep -F passphrase config.yaml | sed -e 's/.* //' -e 's/"//g')
	export C4GH_PASSPHRASE

	dcf=$(mktemp)

	decsha256sum=$(crypt4gh decrypt --sk c4gh.sec.pem <"$file" | LANG=C dd bs=4M 2>"$dcf" | sha256sum | cut -d' ' -f 1)
	decmd5sum=$(crypt4gh decrypt --sk c4gh.sec.pem <"$file" | md5sum | cut -d' ' -f 1)

	decryptedfilesize=$(sed -ne 's/^\([0-9][0-9]*\) bytes (.*) copied, .*$/\1/p' "$dcf")
	rm -f "$dcf"

	curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		--data-binary "$(echo '{
						"vhost":"test",
						"name":"sda",
						"properties":{
							"delivery_mode":2,
							"correlation_id":"CORRID",
							"content_encoding":"UTF-8",
							"content_type":"application/json"
						},
						"routing_key":"files",
						"payload_encoding":"string",
						"payload":"{
							\"type\":\"ingest\",
							\"user\":\"test\",
							\"filepath\":\"FILENAME\",
							\"encrypted_checksums\":[
								{
									\"type\":\"sha256\",
									\"value\":\"SHA256SUM\"
								},
								{
									\"type\":\"md5\",
									\"value\":\"MD5SUM\"
								}
							]
						}"
						}' | sed -e "s/FILENAME/$file/" -e "s/MD5SUM/${md5sum}/" -e "s/SHA256SUM/${sha256sum}/" -e "s/CORRID/$count/" | tr -d '[:space:]' )"

	RETRY_TIMES=0
	until docker logs ingest --since="$now" 2>&1 | grep "File marked as archived"; do
		echo "waiting for ingestion to complete"
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ "$RETRY_TIMES" -eq 60 ]; then
			echo "::error::Time out while waiting for ingest to complete, logs:"

			echo
			echo ingest
			echo

			docker logs --since="$now" ingest
			exit 1
		fi
		sleep 10
	done

	RETRY_TIMES=0
	until docker logs verify --since="$now" 2>&1 | grep "File marked completed"; do
		echo "waiting for verification to complete"
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ "$RETRY_TIMES" -eq 60 ]; then
			echo "::error::Time out while waiting for verify to complete, logs:"

			echo
			echo ingest
			echo

			docker logs --since="$now" ingest

			echo
			echo verify
			echo

			docker logs --since="$now" verify
			exit 1

		fi
		sleep 10
	done

	RETRY_TIMES=0
	until docker logs verify --since="$now" 2>&1 | grep "Removed file from inbox"; do
		echo "waiting for verify to remove file from inbox"
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ "$RETRY_TIMES" -eq 10 ]; then
			echo "::error::Time out while waiting for verify to remove file, logs:"

			echo
			echo verify
			echo

			docker logs --since="$now" verify
			exit 1
		fi
		sleep 10
	done

	now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	access=$(printf "EGAF%05d%06d" "$RANDOM" "$count")

	filepath=$(
		curl --cacert certs/ca.pem -u test:test 'https://localhost:15672/api/queues/test/verified/get' \
			-H 'Content-Type: application/json;charset=UTF-8' \
			-d '{"count":1,"ackmode":"ack_requeue_false","encoding":"auto","truncate":50000}' |
			jq -r '.[0]["payload"]' | jq -r '.["filepath"]'
	)

	# Publish accession id
	curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		--data-binary "$(echo '{
						"vhost":"test",
						"name":"sda",
						"properties":{
							"delivery_mode":2,
							"correlation_id":"CORRID",
							"content_encoding":"UTF-8",
							"content_type":"application/json"
						},
						"routing_key":"files",
						"payload_encoding":"string",
						"payload":"{
							\"type\":\"accession\",
							\"user\":\"test\",
							\"filepath\":\"FILENAME\",
							\"accession_id\":\"ACCESSIONID\",
							\"decrypted_checksums\":[
								{
									\"type\":\"sha256\",
									\"value\":\"DECSHA256SUM\"
								},
								{
									\"type\":\"md5\",
									\"value\":\"DECMD5SUM\"
								}
							]
						}"
						}' | sed -e "s/FILENAME/$filepath/" -e "s/DECMD5SUM/${decmd5sum}/" -e "s/DECSHA256SUM/${decsha256sum}/" -e "s/ACCESSIONID/${access}/" -e "s/CORRID/$count/" | tr -d '[:space:]' )"

	echo "Waiting for finalize/backup to complete"

	# Wait for completion message
	RETRY_TIMES=0
	until curl --cacert certs/ca.pem -u test:test 'https://localhost:15672/api/queues/test/completed/get' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		-d '{"count":1,"ackmode":"ack_requeue_false","encoding":"auto","truncate":50000}' |
		jq -r '.[0]["payload"]' | jq -r '.["filepath"]' | grep -q "$file"; do
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ $RETRY_TIMES -eq 60 ]; then
			echo "::error::Time out while waiting for finalize/backup to complete, logs:"

			echo
			echo ingest
			echo

			docker logs --since="$now" ingest

			echo
			echo verify
			echo

			docker logs --since="$now" verify

			echo
			echo finalize
			echo

			docker logs --since="$now" finalize

			echo
			echo backup
			echo

			docker logs --since="$now" backup
			exit 1
		fi
		sleep 10
	done

	echo
	echo finalize
	echo

	docker logs finalize --since="$now" 2>&1

	echo
	echo backup
	echo

	docker logs backup --since="$now" 2>&1

	dataset=$(printf "EGAD%011d" "$count")

	# Check that it showed up in the database as well

	RETRY_TIMES=0
	statusindb=''

	until [ -n "$statusindb" ]; do
		statusindb=$(docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
			-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
			neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
			-t -A -c "SELECT id FROM local_ega.files where stable_id='$access' AND status='READY';")
		sleep 3
		RETRY_TIMES=$((RETRY_TIMES + 1))

		if [ "$RETRY_TIMES" -eq 150 ]; then
			echo "Timed out waiting correct status in database, aborting"
			exit 1
		fi
	done

	# Map dataset ids
	curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		--data-binary "$(echo '{
						"vhost":"test",
						"name":"sda",
						"properties":{
							"delivery_mode":2,
							"correlation_id":"CORRID",
							"content_encoding":"UTF-8",
							"content_type":"application/json"
						},
						"routing_key":"files",
						"payload_encoding":"string",
						"payload":"{
							\"type\":\"mapping\",
							\"dataset_id\":\"DATASET\",
							\"accession_ids\":[\"ACCESSIONID\"]}"
						}' | sed -e "s/DATASET/$dataset/" -e "s/ACCESSIONID/$access/" -e "s/CORRID/$count/" | tr -d '[:space:]' )"

	RETRY_TIMES=0
	dbcheck=''

	until [ "${#dbcheck}" -ne 0 ]; do

		dbcheck=$(docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
			-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
			neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
			-t -c "SELECT * from local_ega_ebi.file_dataset where dataset_id='$dataset' and file_id='$access'")

		if [ "${#dbcheck}" -eq 0 ]; then

			sleep 10
			RETRY_TIMES=$((RETRY_TIMES + 1))

			if [ "$RETRY_TIMES" -eq 60 ]; then

				echo "Mappings failed"
				docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
					-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
					neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
					-t -c "SELECT * from local_ega_ebi.file_dataset ORDER BY id DESC"

				docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
					-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
					neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
					-t -c "SELECT * from local_ega_ebi.filedataset ORDER BY id DESC"

				docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
					-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
					neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
					-t -c "SELECT id, status, stable_id, archive_path FROM local_ega.files ORDER BY id DESC"

				echo "::error::Timed out waiting for mapper to complete, logs:"

				echo
				echo ingest
				echo

				docker logs --since="$now" ingest

				echo
				echo verify
				echo

				docker logs --since="$now" verify

				echo
				echo finalize
				echo

				docker logs --since="$now" finalize

				echo
				echo mapper
				echo

				docker logs --since="$now" mapper

				exit 1
			fi
		fi
	done

	RETRY_TIMES=0
	decryptedsizedb=''

	until [ -n "$decryptedsizedb" ]; do
		decryptedsizedb=$(docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
			-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
			neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
			-t -A -c "SELECT decrypted_file_size from local_ega.files where stable_id='$access';")
		sleep 3
		RETRY_TIMES=$((RETRY_TIMES + 1))

		if [ "$RETRY_TIMES" -eq 150 ]; then
			echo "Timed out waiting for database to come back, aborting"
			exit 1
		fi
	done

	if [ "$decryptedsizedb" -eq "$decryptedfilesize" ]; then
		# Use this logic to handle case of bad output from db (missing)
		:
	else
		echo "File passed through flow but decrypted size in DB did not match real decrypted size."
		exit 1
	fi

	RETRY_TIMES=0
	decryptedchecksum=''

	until [ -n "$decryptedchecksum" ]; do
		decryptedchecksum=$(docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
			-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
			neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
			-t -A -c "SELECT decrypted_file_checksum from local_ega.files where stable_id='$access';")
		sleep 3
		RETRY_TIMES=$((RETRY_TIMES + 1))

		if [ "$RETRY_TIMES" -eq 150 ]; then
			echo "Timed out waiting for database to come back, aborting"
			exit 1
		fi
	done

	if [ "$decryptedchecksum" = "$decsha256sum" ]; then
		# Use this logic to handle case of bad output from db (missing)
		:
	else
		echo "File passed through flow but decrypted checksum in DB did not match real decrypted checksum."
		exit 1
	fi

	count=$((count + 1))
done

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT * from local_ega_ebi.file_dataset"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT * from local_ega_ebi.filedataset ORDER BY id DESC"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "SELECT id, status, stable_id, archive_path FROM local_ega.files ORDER BY id DESC"
