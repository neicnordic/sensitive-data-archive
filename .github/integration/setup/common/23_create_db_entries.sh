#!/bin/bash

cd dev_utils || exit 1

chmod 600 certs/client-key.pem

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "INSERT INTO local_ega.main (id, stable_id, status, submission_file_path, submission_file_extension, submission_file_calculated_checksum, submission_file_calculated_checksum_type, submission_file_size, submission_user, archive_file_reference, archive_file_type, archive_file_size, archive_file_checksum, archive_file_checksum_type, decrypted_file_size, decrypted_file_checksum, decrypted_file_checksum_type, encryption_method, version, header, created_by, last_modified_by, created_at, last_modified) VALUES (1, 'urn:neic:001-002', 'READY', 'dummy_data.c4gh', 'c4gh', '5e9c767958cc3f6e8d16512b8b8dcab855ad1e04e05798b86f50ef600e137578', 'SHA256', NULL, 'test', '4293c9a7-dc50-46db-b79a-27ddc0dad1c6', NULL, 1049081, '74820dbcf9d30f8ccd1ea59c17d5ec8a714aabc065ae04e46ad82fcf300a731e', 'SHA256', 1048605, '06bb0a514b26497b4b41b30c547ad51d059d57fb7523eb3763cfc82fdb4d8fb7', 'SHA256', 'CRYPT4GH', NULL, '637279707434676801000000010000006c000000000000006af1407abc74656b8913a7d323c4bfd30bf7c8ca359f74ae35357acef29dc5073799e207ec5d022b2601340585ff082565e55fbff5b6cdbbbe6b12a0d0a19ef325a219f8b62344325e22c8d26a8e82e45f053f4dcee10c0ec4bb9e466d5253f139dcd4be', 'lega_in', 'lega_in', '2021-12-13 16:18:06.512169+00', '2021-12-13 16:18:06.512169+00');"

docker run --rm --name client --network dev_utils_default -v "$PWD/certs:/certs" \
	-e PGSSLCERT=/certs/client.pem -e PGSSLKEY=/certs/client-key.pem -e PGSSLROOTCERT=/certs/ca.pem \
	neicnordic/pg-client:latest postgresql://postgres:rootpassword@db:5432/lega \
	-t -c "INSERT INTO local_ega_ebi.filedataset (id, file_id, dataset_stable_id) VALUES (1, 1, 'https://doi.example/ty009.sfrrss/600.45asasga');"
