#!/bin/bash

cd "$(dirname "$0")/../.." || exit 1

PR_NUMBER=$(date +%F)
export PR_NUMBER

storage_types=("posix" "s3")

show_spinner() {
  printf "\033[33;7mStarting up services...\033[0m\n"
  for ((i = 0; i < 25; i++)); do
    for s in / - \\ \|; do
      printf "\r\033[33m%s\033[0m" "$s"
      sleep 0.1
    done
  done
  echo ""
}

for storage_type in "${storage_types[@]}"; do
  printf "\033[0;35mRunning test for %s \033[0m\n" "$storage_type"
  docker compose -f ../.github/integration/sda-doa-"$storage_type"-outbox.yml -p sda-doa up -d
  show_spinner


  for script in "test/setup"/*.sh; do
    echo "Running $script..."
    bash "$script"
  done

  if [[ $storage_type == "posix" ]]; then
    export OUTBOX_TYPE="POSIX"
  else
    export OUTBOX_TYPE="S3"
  fi

  if ! mvn test; then
      echo "Tests failed for $storage_type. Stopping."
  fi

  docker compose -f ../.github/integration/sda-doa-"$storage_type"-outbox.yml down -v
	unset OUTBOX_TYPE
	[[ $storage_type == "posix" ]] && rm -rf outbox

  rm -rf test/crypt4gh
done
unset PR_NUMBER

