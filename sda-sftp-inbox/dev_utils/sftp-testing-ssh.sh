#!/bin/sh

mykey=$(pwd)/src/test/resources/id_ed25519
usr=dummy
host=localhost
port=2222
tmpfile=$(pwd)/README.md

sftp -i "${mykey}" -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -o KbdInteractiveAuthentication=no -P ${port} ${usr}@${host} <<EOF 
  put ${tmpfile}
  dir
  ls -al
  exit
EOF
ST=$?

if test $ST -ne 0
then
  echo SFTP LOGIN FAILURE. RC=${ST} 1>&2
  exit $ST
fi

exit $ST
