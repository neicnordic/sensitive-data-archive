#!/bin/bash

cd dev_utils || exit 1

for file in dummy_data.c4gh largefile.c4gh empty.c4gh truncated1.c4gh truncated2.c4gh wrongly_encrypted.c4gh test_db_file.c4gh test_finalize_file.c4gh; do
    docker run --rm -v "$(pwd)":/src -v dev_utils_inbox:/inbox alpine sh -c "cp /src/$file /inbox && chmod -R 777 /inbox"
done

docker run --rm -v "$(pwd)":/src -v dev_utils_inbox:/inbox alpine sh -c "ls -al /inbox"
