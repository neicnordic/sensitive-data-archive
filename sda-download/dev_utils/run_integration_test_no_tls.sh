#!/bin/bash

cd ..

export STORAGETYPE=s3notls

find .github/integration/setup/{common,s3notls}/*.sh 2>/dev/null | sort -t/ -k5 -n | while read -r runscript; do
  echo "Executing setup script $runscript";
  bash -x "$runscript";
done

find .github/integration/tests/{common,s3notls}/*.sh 2>/dev/null | sort -t/ -k5 -n | while read -r runscript; do
  echo "Executing test script $runscript";
  bash -x "$runscript";
done
