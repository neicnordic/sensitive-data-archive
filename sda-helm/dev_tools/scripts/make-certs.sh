#!/bin/sh

basedir="sda-deploy-init/config/certs"
days=1200

mkdir -p "${basedir}"

# create CA certificate
openssl req -config "$(dirname "$0")"/ssl.cnf -new -sha256 -nodes -extensions v3_ca -out "./${basedir}/ca.csr" -keyout "./${basedir}/ca.key"
openssl req -config "$(dirname "$0")"/ssl.cnf -key "./${basedir}/ca.key" -x509 -new -days 7300 -sha256 -nodes -extensions v3_ca -out "./${basedir}/ca.crt"

# Create certificate for MQ
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/server.key" -out "./${basedir}/mq.csr" -extensions mq_cert
openssl x509 -req -in "./${basedir}/mq.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/server.crt" -extensions mq_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for DB
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/pg.key" -out "./${basedir}/pg.csr" -extensions db_cert
openssl x509 -req -in "./${basedir}/pg.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/pg.crt" -extensions db_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for minio
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/private.key" -out "./${basedir}/s3.csr" -extensions minio_cert
openssl x509 -req -in "./${basedir}/s3.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/public.crt" -extensions minio_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create client certificate
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/client.key" -out "./${basedir}/client.csr" -extensions client_cert -subj "/CN=lega_in/CN=admin/" 
openssl x509 -req -in "./${basedir}/client.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/client.crt" -extensions client_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for inbox
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/inbox.key" -out "./${basedir}/inbox.csr" -extensions inbox_cert -subj "/CN=lega_in/CN=admin/" 
openssl x509 -req -in "./${basedir}/inbox.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/inbox.crt" -extensions inbox_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for ingest
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/ingest.key" -out "./${basedir}/ingest.csr" -extensions ingest_cert -subj "/CN=lega_in/CN=admin/" 
openssl x509 -req -in "./${basedir}/ingest.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/ingest.crt" -extensions ingest_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for intercept
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/intercept.key" -out "./${basedir}/intercept.csr" -extensions intercept_cert -subj "/CN=admin" 
openssl x509 -req -in "./${basedir}/intercept.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/intercept.crt" -extensions intercept_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for finalize
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/finalize.key" -out "./${basedir}/finalize.csr" -extensions finalize_cert -subj "/CN=lega_in/CN=admin"
openssl x509 -req -in "./${basedir}/finalize.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/finalize.crt" -extensions finalize_cert -extfile "$(dirname "$0")"/ssl.cnf 

# Create certificate for verify
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/verify.key" -out "./${basedir}/verify.csr" -extensions verify_cert -subj "/CN=lega_in/CN=admin/" 
openssl x509 -req -in "./${basedir}/verify.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/verify.crt" -extensions verify_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for doa
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/doa.key" -out "./${basedir}/doa.csr" -extensions doa_cert -subj "/CN=lega_out/CN=admin/"
openssl x509 -req -in "./${basedir}/doa.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/doa.crt" -extensions doa_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for download
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/download.key" -out "./${basedir}/download.csr" -extensions download_cert -subj "/CN=lega_out/CN=admin/"
openssl x509 -req -in "./${basedir}/download.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/download.crt" -extensions download_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for orch
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/orch.key" -out "./${basedir}/orch.csr" -extensions orch_cert -subj "/CN=admin" 
openssl x509 -req -in "./${basedir}/orch.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/orch.crt" -extensions orch_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for mapper
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/mapper.key" -out "./${basedir}/mapper.csr" -extensions mapper_cert -subj "/CN=lega_out/CN=admin"
openssl x509 -req -in "./${basedir}/mapper.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/mapper.crt" -extensions mapper_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for backup
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/backup.key" -out "./${basedir}/backup.csr" -extensions backup_cert -subj "/CN=admin/CN=lega_in"
openssl x509 -req -in "./${basedir}/backup.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/backup.crt" -extensions backup_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for auth
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/auth.key" -out "./${basedir}/auth.csr" -extensions auth_cert
openssl x509 -req -in "./${basedir}/auth.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/auth.crt" -extensions auth_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for tester
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/tester.key" -out "./${basedir}/tester.csr" -extensions tester_cert -subj "/CN=lega_in/CN=admin/" 
openssl x509 -req -in "./${basedir}/tester.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/tester.crt" -extensions tester_cert -extfile "$(dirname "$0")"/ssl.cnf

# Create certificate for cega
openssl req -config "$(dirname "$0")"/ssl.cnf -new -nodes -newkey rsa:4096 -keyout "./${basedir}/cega.key" -out "./${basedir}/cega.csr" -extensions cega
openssl x509 -req -in "./${basedir}/cega.csr" -days "${days}" -CA "./${basedir}/ca.crt" -CAkey "./${basedir}/ca.key" -set_serial 01 -out "./${basedir}/cega.crt" -extensions cega -extfile "$(dirname "$0")"/ssl.cnf

# Create token
openssl req  -nodes -new -x509  -keyout "./${basedir}/token.key" -out "./${basedir}/token.pub" -config "$(dirname "$0")"/ssl.cnf

chmod 644 "./${basedir}/"*
