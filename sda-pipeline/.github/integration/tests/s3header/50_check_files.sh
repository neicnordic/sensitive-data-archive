#!/bin/bash

cd dev_utils || exit 1

chmod 600 certs/client-key.pem

function db_query() {
	docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://lega_in:lega_in@db:5432/lega \
	-t -A -c "$1"
}

echo "Checking archive files in s3"

# Earlier tests verify that the file is in the database correctly
# even though the files are disabled we can still get them from the db
accessids=$(db_query "SELECT stable_id FROM local_ega.files where status='DISABLED';")

if [ -z "$accessids" ]; then
	echo "Failed to get accession ids"
	exit 1
fi

for aid in $accessids; do
	echo "Checking accesssionid $aid"

	apath=$(db_query "SELECT archive_path FROM local_ega.files where stable_id='$aid';")

	acheck=$(db_query "SELECT archive_file_checksum FROM local_ega.files where stable_id='$aid';")

	achecktype=$(db_query "SELECT archive_file_checksum_type FROM local_ega.files where stable_id='$aid';")

	decrcheck=$(db_query "SELECT decrypted_file_checksum FROM local_ega.files where stable_id='$aid';")

	s3cmd -c s3cmd.conf get "s3://archive/$apath" "tmp/$apath"

	if [ "${achecktype,,*}" = 'md5sum' ]; then
		filecheck=$(md5sum "tmp/$apath" | cut -d' ' -f1)
	else
		filecheck=$(sha256sum "tmp/$apath" | cut -d' ' -f1)
	fi

	if [ "$filecheck" != "$acheck" ]; then
		echo "Checksum archive failure for file with stable id $aid ($filecheck vs $acheck)"
		exit 1
	fi

	rm -f "tmp/$apath"

	apath=$(db_query "SELECT inbox_path FROM local_ega.files where stable_id='$aid';")

	# Check checksum for backuped copy as well
	s3cmd -c s3cmd.conf get "s3://backup/$apath" "tmp/$apath"

	# Decrypt the file
	crypt4gh decrypt --sk c4gh-new.sec.pem < "tmp/$apath" > "tmp/$apath-decr"

	if [ "${achecktype,,*}" = 'md5sum' ]; then
		filecheck=$(md5sum "tmp/$apath-decr" | cut -d' ' -f1)
	else
		filecheck=$(sha256sum "tmp/$apath-decr" | cut -d' ' -f1)
	fi

	if [ "$filecheck" != "$decrcheck" ]; then
		echo "Checksum backup failure for file with stable id $aid ($filecheck vs $decrcheck)"
		exit 1
	fi

	rm -f "tmp/$apath" "tmp/$apath-decr"
done

echo "Passed check for archive and backup (s3 with header)"
