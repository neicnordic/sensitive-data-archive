#!/bin/bash

usr=dummy
host=localhost
port=2222
tmpfile=$(pwd)/README.md

expect -c "
spawn sftp -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -P ${port} ${usr}@${host}
expect \"${usr}@${host}'s password:\"
send \"password\r\"
expect \"sftp>\"
send \"put ${tmpfile}\n\"
send \"exit\r\"
interact "
