#!/bin/bash

if [ "$STORAGETYPE" = s3notls ] || [ "$STORAGETYPE" = s3notlsheader ] || [ "$STORAGETYPE" = cancel ]; then
    exit 0
fi

cd dev_utils || exit 1

#
# Submit some messages that will trigger various failures. Do this
# before the "real" work to verify that these failures are not top of
# queue and handled again and again.
#

# Helper function that checks if msg is moved to error queue
# admits a string arg that is part of the output error msg

function check_move_to_error_queue() {
	now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	echo "$now"
	RETRY_TIMES=0
	echo
	echo "Waiting for msg containing \"""$1""\" to move to error queue."
	until curl --cacert certs/ca.pem  -u test:test 'https://localhost:15672/api/queues/test/error/get' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		-d '{"count":1,"ackmode":"ack_requeue_false","encoding":"auto","truncate":50000}' 2>&1 | grep -q "$1"; do
		printf '%s' "."
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ $RETRY_TIMES -eq 61 ]; then
			echo "::error::Time out while waiting for msg to move to error queue, logs:"
			for k in ingest verify finalize backup; do
				echo
				echo "$k"
				echo
				docker logs --since="$now" "$k"
			done
			exit 1
		fi
		sleep 2
	done
	echo
	echo "Message with \"""$1""\" moved to error queue."
	echo
}

# Submit a file encrypted with the wrong key

md5sum=$(md5sum wrongly_encrypted.c4gh | cut -d' ' -f 1)
sha256sum=$(sha256sum wrongly_encrypted.c4gh | cut -d' ' -f 1)

curl --cacert certs/ca.pem  -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
     -H 'Content-Type: application/json;charset=UTF-8' \
     --data-binary "$(echo '{
    	                       "vhost":"test",
                               "name":"sda",
    	                       "properties":{
    	                                     "delivery_mode":2,
    	                                     "correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
    	                                     "content_encoding":"UTF-8",
    	                                     "content_type":"application/json"
    	                                    },
    	                       "routing_key":"files",
    	                       "payload_encoding":"string",
    	                       "payload":"{
    	                                   \"type\":\"ingest\",
    	                                   \"user\":\"test\",
    	                                   \"filepath\":\"/wrongly_encrypted.c4gh\",
    	                                   \"encrypted_checksums\":[{
    	                                                             \"type\":\"sha256\",
    	                                                             \"value\":\"SHA256SUM\"},
    	                                                            {
    	                                                             \"type\":\"md5\",
    	                                                             \"value\":\"MD5SUM\"
    	                                                            }
    	                                                           ]
    	                                  }"
    	                      }' | sed -e "s/SHA256SUM/${sha256sum}/" -e "s/MD5SUM/${md5sum}/" | tr -d '[:space:]' )"

# Verify that message is moved to the error queue

check_move_to_error_queue "decryption failed"

# Submit a non-existent file

curl --cacert certs/ca.pem  -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
     -H 'Content-Type: application/json;charset=UTF-8' \
     --data-binary "$(echo '{
    	                       "vhost":"test",
                               "name":"sda",
    	                       "properties":{
    	                                     "delivery_mode":2,
    	                                     "correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
    	                                     "content_encoding":"UTF-8",
    	                                     "content_type":"application/json"
    	                                    },
    	                       "routing_key":"files",
    	                       "payload_encoding":"string",
    	                       "payload":"{
    	                                   \"type\":\"ingest\",
    	                                   \"user\":\"test\",
    	                                   \"filepath\":\"/THISFILEDOESNOTEXIST\",
    	                                   \"encrypted_checksums\":[{
    	                                                             \"type\":\"sha256\",
    	                                                             \"value\":\"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\"},
    	                                                            {
    	                                                             \"type\":\"md5\",
    	                                                             \"value\":\"d41d8cd98f00b204e9800998ecf8427e\"
    	                                                            }
    	                                                           ]
    	                                  }"
    	                      }' | tr -d '[:space:]' )"


# Verify that message is moved to the error queue (takes a few mins).

if [ "$STORAGETYPE" = posix ] || [ "$STORAGETYPE" = posixheader ]; then
	check_move_to_error_queue "no such file or directory"
else
	check_move_to_error_queue "NoSuchKey: The specified key does not exist."
fi

: <<'END_COMMENT'
# (currently not implemented)
# Submit an existing file but incorrect checksum

curl --cacert certs/ca.pem  -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
     -H 'Content-Type: application/json;charset=UTF-8' \
     --data-binary '{
    	                       "vhost":"test",
                               "name":"sda",
    	                       "properties":{
    	                                     "delivery_mode":2,
    	                                     "correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
    	                                     "content_encoding":"UTF-8",
    	                                     "content_type":"application/json"
    	                                    },
    	                       "routing_key":"files",
    	                       "payload_encoding":"string",
    	                       "payload":"{
    	                                   \"type\":\"ingest\",
    	                                   \"user\":\"test\",
    	                                   \"filepath\":\"/dummy_data.c4gh\",
    	                                   \"encrypted_checksums\":[{
    	                                                             \"type\":\"sha256\",
    	                                                             \"value\":\"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\"},
    	                                                            {
    	                                                             \"type\":\"md5\",
    	                                                             \"value\":\"d41d8cd98f00b204e9800998ecf8427e\"
    	                                                            }
    	                                                           ]
    	                                  }"
    	                      }'


# Verify message put in error here once https://github.com/neicnordic/sda-pipeline/issues/130 is resolved.

curl --cacert certs/ca.pem  -u test:test 'https://localhost:15672/api/queues/test/error/get' \
		   -H 'Content-Type: application/json;charset=UTF-8' \
		   -d '{"count":1,"ackmode":"ack_requeue_true","encoding":"auto","truncate":50000}'

END_COMMENT

# Submit a truncated file (with correct checksum)

md5sum=$(md5sum truncated1.c4gh | cut -d' ' -f 1)
sha256sum=$(sha256sum truncated1.c4gh | cut -d' ' -f 1)

curl --cacert certs/ca.pem  -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
     -H 'Content-Type: application/json;charset=UTF-8' \
     --data-binary "$( echo '{
    	                       "vhost":"test",
                               "name":"sda",
    	                       "properties":{
    	                                     "delivery_mode":2,
    	                                     "correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
    	                                     "content_encoding":"UTF-8",
    	                                     "content_type":"application/json"
    	                                    },
    	                       "routing_key":"files",
    	                       "payload_encoding":"string",
    	                       "payload":"{
    	                                   \"type\":\"ingest\",
    	                                   \"user\":\"test\",
    	                                   \"filepath\":\"/truncated1.c4gh\",
    	                                   \"encrypted_checksums\":[{
    	                                                             \"type\":\"sha256\",
    	                                                             \"value\":\"SHA256SUM\"},
    	                                                            {
    	                                                             \"type\":\"md5\",
    	                                                             \"value\":\"MD5SUM\"
    	                                                            }
    	                                                           ]
    	                                  }"
    	                      }' | sed -e "s/SHA256SUM/${sha256sum}/" -e "s/MD5SUM/${md5sum}/" | tr -d '[:space:]' )"

# Verify that message is moved to the error queue.

check_move_to_error_queue "data segment can't be decrypted with any of header keys"

# Submit a smaller truncated file (with correct checksum)

md5sum=$(md5sum truncated2.c4gh | cut -d' ' -f 1)
sha256sum=$(sha256sum truncated2.c4gh | cut -d' ' -f 1)

curl --cacert certs/ca.pem  -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
     -H 'Content-Type: application/json;charset=UTF-8' \
     --data-binary "$( echo '{
    	                       "vhost":"test",
                               "name":"sda",
    	                       "properties":{
    	                                     "delivery_mode":2,
    	                                     "correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
    	                                     "content_encoding":"UTF-8",
    	                                     "content_type":"application/json"
    	                                    },
    	                       "routing_key":"files",
    	                       "payload_encoding":"string",
    	                       "payload":"{
    	                                   \"type\":\"ingest\",
    	                                   \"user\":\"test\",
    	                                   \"filepath\":\"/truncated2.c4gh\",
    	                                   \"encrypted_checksums\":[{
    	                                                             \"type\":\"sha256\",
    	                                                             \"value\":\"SHA256SUM\"},
    	                                                            {
    	                                                             \"type\":\"md5\",
    	                                                             \"value\":\"MD5SUM\"
    	                                                            }
    	                                                           ]
    	                                  }"
    	                      }' | sed -e "s/SHA256SUM/${sha256sum}/" -e "s/MD5SUM/${md5sum}/" | tr -d '[:space:]' )"

# Verify that message is moved to the error queue.

check_move_to_error_queue "unexpected EOF"

if [ "$STORAGETYPE" != sftpheader ]; then

	# Submit a correct file and then cause duplicate accession id.

	now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

	C4GH_PASSPHRASE=$(grep -F passphrase config.yaml | sed -e 's/.* //' -e 's/"//g')
	export C4GH_PASSPHRASE

	dcf=$(mktemp)

	md5sum=$(md5sum test_finalize_file.c4gh | cut -d' ' -f 1)
	sha256sum=$(sha256sum test_finalize_file.c4gh | cut -d' ' -f 1)
	decsha256sum=$(crypt4gh decrypt --sk c4gh.sec.pem < test_finalize_file.c4gh | LANG=C dd bs=4M 2>"$dcf" | sha256sum | cut -d' ' -f 1)
	decmd5sum=$(crypt4gh decrypt --sk c4gh.sec.pem < test_finalize_file.c4gh | md5sum | cut -d' ' -f 1)


	curl --cacert certs/ca.pem  -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		--data-binary "$( echo '{
								"vhost":"test",
								"name":"sda",
								"properties":{
												"delivery_mode":2,
												"correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
												"content_encoding":"UTF-8",
												"content_type":"application/json"
												},
								"routing_key":"files",
								"payload_encoding":"string",
								"payload":"{
											\"type\":\"ingest\",
											\"user\":\"test\",
											\"filepath\":\"/test_finalize_file.c4gh\",
											\"encrypted_checksums\":[{
																		\"type\":\"sha256\",
																		\"value\":\"SHA256SUM\"},
																		{
																		\"type\":\"md5\",
																		\"value\":\"MD5SUM\"
																		}
																	]
											}"
								}' | sed -e "s/SHA256SUM/${sha256sum}/" -e "s/MD5SUM/${md5sum}/" | tr -d '[:space:]' )"

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

	now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
	access=$(printf "EGAF%05d%06d" "$RANDOM" 1)

	# Publish accession id
	curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		--data-binary "$(echo '{
						"vhost":"test",
						"name":"sda",
						"properties":{
							"delivery_mode":2,
							"correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
							"content_encoding":"UTF-8",
							"content_type":"application/json"
						},
						"routing_key":"files",
						"payload_encoding":"string",
						"payload":"{
							\"type\":\"accession\",
							\"user\":\"test\",
							\"filepath\":\"/test_finalize_file.c4gh\",
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
						}' | sed -e "s/DECMD5SUM/${decmd5sum}/" -e "s/DECSHA256SUM/${decsha256sum}/" -e "s/ACCESSIONID/${access}/" | tr -d '[:space:]' )"

	echo "Waiting for finalize/backup to complete"

	# Wait for completion message
	RETRY_TIMES=0
	until curl --cacert certs/ca.pem -u test:test 'https://localhost:15672/api/queues/test/completed/get' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		-d '{"count":1,"ackmode":"ack_requeue_false","encoding":"auto","truncate":50000}' |
		jq -r '.[0]["payload"]' | jq -r '.["filepath"]' | grep -q test_finalize_file.c4gh; do
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


	# Publish accession id again
	curl --cacert certs/ca.pem -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
		-H 'Content-Type: application/json;charset=UTF-8' \
		--data-binary "$(echo '{
						"vhost":"test",
						"name":"sda",
						"properties":{
							"delivery_mode":2,
							"correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
							"content_encoding":"UTF-8",
							"content_type":"application/json"
						},
						"routing_key":"files",
						"payload_encoding":"string",
						"payload":"{
							\"type\":\"accession\",
							\"user\":\"test\",
							\"filepath\":\"/test_finalize_file.c4gh\",
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
						}' | sed -e "s/DECMD5SUM/${decmd5sum}/" -e "s/DECSHA256SUM/${decsha256sum}/" -e "s/ACCESSIONID/${access}/" | tr -d '[:space:]' )"


	# Verify that message is moved to the error queue.

	check_move_to_error_queue "There is a conflict regarding the file accessionID"

fi

# Submit a correct file and then cause a db query failure.

now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

docker pause verify &> /dev/null

md5sum=$(md5sum test_db_file.c4gh | cut -d' ' -f 1)
sha256sum=$(sha256sum test_db_file.c4gh | cut -d' ' -f 1)

curl --cacert certs/ca.pem  -vvv -u test:test 'https://localhost:15672/api/exchanges/test/sda/publish' \
     -H 'Content-Type: application/json;charset=UTF-8' \
     --data-binary "$( echo '{
    	                       "vhost":"test",
                               "name":"sda",
    	                       "properties":{
    	                                     "delivery_mode":2,
    	                                     "correlation_id":"8bc6a0ef-2a64-4d50-ab09-59fc789f5186",
    	                                     "content_encoding":"UTF-8",
    	                                     "content_type":"application/json"
    	                                    },
    	                       "routing_key":"files",
    	                       "payload_encoding":"string",
    	                       "payload":"{
    	                                   \"type\":\"ingest\",
    	                                   \"user\":\"test\",
    	                                   \"filepath\":\"/test_db_file.c4gh\",
    	                                   \"encrypted_checksums\":[{
    	                                                             \"type\":\"sha256\",
    	                                                             \"value\":\"SHA256SUM\"},
    	                                                            {
    	                                                             \"type\":\"md5\",
    	                                                             \"value\":\"MD5SUM\"
    	                                                            }
    	                                                           ]
    	                                  }"
    	                      }' | sed -e "s/SHA256SUM/${sha256sum}/" -e "s/MD5SUM/${md5sum}/" | tr -d '[:space:]' )"

RETRY_TIMES=0
echo
echo "Waiting for ingest to confirm delivery."
	until docker logs --since="$now" ingest 2>&1 | grep -q "confirmed delivery"; do
		printf '%s' "."
		RETRY_TIMES=$((RETRY_TIMES + 1))
		if [ $RETRY_TIMES -eq 60 ]; then
			echo "::error::Time out while waiting for ingest to confirm delivery, logs:"
			echo
			echo ingest
			echo
			docker logs --since="$now" ingest
			exit 1
		fi
		sleep 1
	done

chmod 600 certs/client-key.pem
docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
			-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
			neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
			-t -A -c "update sda.file_event_log set correlation_id = 'ad8df344-19cc-4452-8331-4c12d47b9804' where correlation_id = '8bc6a0ef-2a64-4d50-ab09-59fc789f5186'"

docker unpause verify &> /dev/null

# Verify that message is moved to the error queue.

check_move_to_error_queue "sql: no rows in result set"

# Cleanup queues
curl --cacert certs/ca.pem  -u test:test -X DELETE 'https://localhost:15672/api/queues/test/completed/contents'
curl --cacert certs/ca.pem  -u test:test -X DELETE 'https://localhost:15672/api/queues/test/verified/contents'
