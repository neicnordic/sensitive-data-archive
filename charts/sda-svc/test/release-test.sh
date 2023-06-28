#!/bin/bash

if [ "$INBOX_STORAGE_TYPE" == "s3" ]; then
	if [ "$TLS" == true ]; then
		cat >> "/tmp/s3cfg" <<-EOF
		host_base = $INBOX_SERVICE_NAME
		host_bucket = $INBOX_SERVICE_NAME
		access_key = test_dummy.org
		access_token = $INBOX_ACCESS_TOKEN
		use_https = True
		ca_certs_file = /tls/ca.crt
		EOF
	else
		cat >> "/tmp/s3cfg" <<-EOF
		host_base = $INBOX_SERVICE_NAME
		host_bucket = $INBOX_SERVICE_NAME
		access_key = test_dummy.org
		access_token = $INBOX_ACCESS_TOKEN
		use_https = False
		EOF
	fi
fi

cat /tmp/s3cfg

if [ "${DEPLOYMENT_TYPE}" = all ] || [ "${DEPLOYMENT_TYPE}" = external ]; then

	if [ "$INBOX_STORAGE_TYPE" == "posix" ] && [ "$TLS" == true ]; then
		# Connect and see that we get a ssh greeting
		if ! echo "test" | nc "$INBOX_SERVICE_NAME" 2222 | grep "SSH-2.0"; then
			echo "Failed inbox verification, bailing out"
			exit 1
		fi
	elif [ "$INBOX_STORAGE_TYPE" == "s3" ]; then
		if [ "$TLS" == true ]; then
			echo "Will try connecting to https://$INBOX_SERVICE_NAME/"
			if ! s3cmd -c "/tmp/s3cfg" ls s3://test_dummy.org ; then
				echo "expected 403 got: $responsecode"
				echo "Failed inbox verification, bailing out"
				exit 1
			fi

			echo "Will try connecting to http://$AUTH_SERVICE_NAME/"
			responsecode=$(curl --cacert /tls/ca.crt -s -o /dev/null -w "%{http_code}" "https://$AUTH_SERVICE_NAME")
			if ! [ "$responsecode" -eq 200 ]; then
				echo "expected 200 got: $responsecode"
				echo "Failed auth verification, bailing out"
				exit 1
			fi
		else
			echo "Will try connecting to http://$INBOX_SERVICE_NAME/"
			if ! s3cmd -c "/tmp/s3cfg" ls s3://test_dummy.org ; then
				echo "Failed inbox verification, bailing out"
				exit 1
			fi

			echo "Will try connecting to http://$AUTH_SERVICE_NAME/"
			responsecode=$(curl -s -o /dev/null -w "%{http_code}" "http://$AUTH_SERVICE_NAME")
			if ! [ "$responsecode" -eq 200 ]; then
				echo "expected 200 got: $responsecode"
				echo "Failed auth verification, bailing out"
				exit 1
			fi
		fi
	else
		echo "Unknown inbox storageType: $INBOX_STORAGE_TYPE, bailing out"
		exit 1
	fi

	echo "Inbox seems okay"

	if [ -n "$DOA_SERVICE_NAME" ] && [ "${TLS}" = true ]; then
		responsecode=$(curl --cacert /tls/ca.crt -s -o /dev/null -w "%{http_code}" "https://$DOA_SERVICE_NAME/metadata/datasets")
		if ! [ "$responsecode" -eq 401 ]; then
			echo "expected 401 got: $responsecode"
			echo "Failed DOA verification, bailing out"
			exit 1
		fi
		echo "DOA seems okay"
	elif [ -n "$DOWNLOAD_SERVICE_NAME" ] ; then
		if [ "${TLS}" = true ]; then
			responsecode=$(curl --cacert /tls/ca.crt -s -o /dev/null -w "%{http_code}" "https://$DOWNLOAD_SERVICE_NAME/health")
			if ! [ "$responsecode" -eq 200 ]; then
				echo "expected 200 got: $responsecode"
				echo "Failed Download verification, bailing out"
				exit 1
			fi
		else
			responsecode=$(curl --cacert /tls/ca.crt -s -o /dev/null -w "%{http_code}" "http://$DOWNLOAD_SERVICE_NAME/health")
			if ! [ "$responsecode" -eq 200 ]; then
				echo "expected 200 got: $responsecode"
				echo "Failed Download verification, bailing out"
				exit 1
			fi
		fi
		echo "Download seems okay"
	else
		echo "No data out solution deployed, bailing out"
		exit 1
	fi

fi

if [ "${DEPLOYMENT_TYPE}" = external ]; then
	echo "External-only deployment, nothing more to check."
	exit 0
fi

if [ "${DEPLOYMENT_TYPE}" = internal ]; then
	echo "Internal-only deployment, nothing more to check."
	exit 0
fi

exit 0
