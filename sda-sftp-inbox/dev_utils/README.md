# Local testing howto

First create the necessary credentials.

```command
cd dev_utils
sh make_certs.sh
```

Start creating the certificates, java is picky and we need to create them first

```command
docker-compose up -d certfixer
```

To start all the other services using docker compose.

```command
docker-compose up -d
```

For a test example use:

```command
cd ../
sh ./dev_utils/sftp-testing-ssh.sh
sh ./dev_utils/sftp-testing-pass.sh
```

For manual testing use:

```
sftp -i src/test/resources/ed25519.sec -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -P 2222 dummy@localhost

# for password auth use: `password` as credential
sftp -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -P 2222 dummy@localhost
```
