#!/bin/sh

echo "Insert File"
docker run --rm --network sda-doa_default -v "sda-doa_client_certs:/certs" \
  -e PGPASSWORD=password \
  -e PGSSLMODE=verify-ca \
  -e PGSSLCERT=/certs/client.crt \
  -e PGSSLKEY=/certs/client.key \
  -e PGSSLROOTCERT=/certs/ca.crt \
  postgres:latest \
  bash -c "psql -h postgres -U lega_in -d sda -c \"SELECT local_ega.insert_file('body.enc', 'requester@elixir-europe.org');\""
echo "Set Header For The File"
docker run --rm --network sda-doa_default -v "sda-doa_client_certs:/certs" \
  -e PGPASSWORD=password \
  -e PGSSLMODE=verify-ca \
  -e PGSSLCERT=/certs/client.crt \
  -e PGSSLKEY=/certs/client.key \
  -e PGSSLROOTCERT=/certs/ca.crt \
  postgres:latest \
  bash -c "psql -h postgres -U lega_in -d sda -c \"UPDATE local_ega.files SET header = '637279707434676801000000010000006c00000000000000aa7ad1bb4f93bf5e4fb3bc28a95bc4d80bf2fd8075e69eb2ee15e0a4f08f1d78ab98c8fd9b50e675f71311936e8d0c6f73538962b836355d5d4371a12eae46addb43518b5236fb9554249710a473026f34b264a61d2ba52ed11abc1efa1d3478fa40a710' WHERE id = 1;\""
echo "Set File Data"
docker run --rm --network sda-doa_default -v "sda-doa_client_certs:/certs" \
  -e PGPASSWORD=password \
  -e PGSSLMODE=verify-ca \
  -e PGSSLCERT=/certs/client.crt \
  -e PGSSLKEY=/certs/client.key \
  -e PGSSLROOTCERT=/certs/ca.crt \
  postgres:latest \
  bash -c "psql -h postgres -U lega_in -d sda -c \"UPDATE local_ega.files SET archive_path = 'test/body.enc', status = 'READY', stable_id = 'EGAF00000000014' WHERE id = 1;\""
echo "Insert Dataset"
docker run --rm --network sda-doa_default -v "sda-doa_client_certs:/certs" \
  -e PGPASSWORD=password \
  -e PGSSLMODE=verify-ca \
  -e PGSSLCERT=/certs/client.crt \
  -e PGSSLKEY=/certs/client.key \
  -e PGSSLROOTCERT=/certs/ca.crt \
  postgres:latest \
  bash -c "psql -h postgres -U lega_out -d sda -c \"INSERT INTO local_ega_ebi.filedataset(file_id, dataset_stable_id) values(1, 'EGAD00010000919');\""
echo "Insert Event Log REGISTERED"
docker run --rm --network sda-doa_default -v "sda-doa_client_certs:/certs" \
  -e PGPASSWORD=password \
  -e PGSSLMODE=verify-ca \
  -e PGSSLCERT=/certs/client.crt \
  -e PGSSLKEY=/certs/client.key \
  -e PGSSLROOTCERT=/certs/ca.crt \
  postgres:latest \
  bash -c "psql -h postgres -U lega_out -d sda -c \"INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES('EGAD00010000919', 'registered', '{\\\"type\\\": \\\"mapping\\\"}')\""
echo "Insert Event Log RELEASED"
docker run --rm --network sda-doa_default -v "sda-doa_client_certs:/certs" \
  -e PGPASSWORD=password \
  -e PGSSLMODE=verify-ca \
  -e PGSSLCERT=/certs/client.crt \
  -e PGSSLKEY=/certs/client.key \
  -e PGSSLROOTCERT=/certs/ca.crt \
  postgres:latest \
  bash -c "psql -h postgres -U lega_out -d sda -c \"INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES('EGAD00010000919', 'released', '{\\\"type\\\": \\\"release\\\"}')\""
echo "Insert Dataset Reference"
docker run --rm --network sda-doa_default -v "sda-doa_client_certs:/certs" \
  -e PGPASSWORD=rootpasswd \
  -e PGSSLMODE=verify-ca \
  -e PGSSLCERT=/certs/client.crt \
  -e PGSSLKEY=/certs/client.key \
  -e PGSSLROOTCERT=/certs/ca.crt \
  postgres:latest \
  bash -c "psql -h postgres -U postgres -d sda -c \"INSERT INTO sda.dataset_references(dataset_id, reference_id, reference_scheme) values('1', 'GDI-NO-10001','GDI');\""
