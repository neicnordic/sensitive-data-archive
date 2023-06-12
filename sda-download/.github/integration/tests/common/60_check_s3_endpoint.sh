#!/bin/bash


TLS="True"
[[ "$STORAGETYPE" == "s3notls" ]] && TLS="False"

#
# Set directories
#

cert_dir="$PWD/dev_utils/certs"

cd "$(mktemp -d)" || exit 1

# Define helper functions

# prints regular text
info() {
    printf "%s\n" "$*" >&2
}

# prints green text
success() {
    printf "\e[32m%s\e[0m\n" "$*" >&2
}

# prints yellow text
warning() {
    printf "\e[1;33m%s\e[0m\n" "$*" >&2
}

# prints red text
error() {
    printf "\e[31m%s\e[0m\n" "$*" >&2
}

# Runs a command given as $@, checks the output and return value, and compares
# them to the $should_contain and $should_return environment variables.
# If the values match the function returns 0, otherwise 1.
run_test() {
    # shellcheck disable=SC2068
    output="$($@ 2>/tmp/cmd_error_log)"
    retval="$?"

    # shellcheck disable=SC2154
    if [[ "$output" == *"$should_contain"* ]] && [[ "$retval" == "$should_return" ]]
    then
        success "PASSED"
    else
        error "FAIL"
        [[ "$output" != *"$should_contain"* ]] && \
            info "Output: '$output' does not contain '$should_contain'"
        [[ "$retval" != "$should_return" ]] && \
            info "Expected return status: '$should_return' but got '$retval'"
        info "Error log: $(cat "/tmp/cmd_error_log")"
        return 1
    fi
    return 0
}

# Returns an oidc token from the mockauth. By default it returns the first token
# but a number given as the first arg can be used to fetch other tokens as well.
# shellcheck disable=SC2120
auth_token() {
    token_num="${1:-0}"
    if [[ "$TLS" == "True" ]]
    then
        curl -sS --cacert "$cert_dir/ca.pem" https://localhost:8000/tokens \
            | jq -r ".[$token_num]"
    else
        curl -sS http://localhost:8000/tokens \
            | jq -r ".[$token_num]"
    fi
}

#
# Create s3cmd configs
#

port="8443"
[[ "$TLS" == "False" ]] && port="8080"

cat << EOF >s3cmd.valid
[default]
access_key=access
secret_key=secretkey
check_ssl_certificate = False
encoding = UTF-8
encrypt = False
guess_mime_type = True
host_base = localhost:$port/s3/
host_bucket = localhost:$port/s3/
human_readable_sizes = True
multipart_chunk_size_mb = 5
use_https = $TLS
socket_timeout = 30
ca_certs_file = $cert_dir/ca.pem
access_token = $(auth_token)
EOF

cp s3cmd.valid s3cmd.invalid
sed -i "s/\(access_token = \).*/\1invalid_token/g" s3cmd.invalid

#
# Run tests
#

echo
info " ----- S3 Tests -------------------------------------------------------- "
echo

dataset="https://doi.example/ty009.sfrrss/600.45asasga"

## Test s3cmd listing with valid token
export should_contain="s3://$dataset"
export should_return="0"

info 'Testing dataset listing with valid token'
run_test s3cmd -c s3cmd.valid ls s3://

# Test s3cmd listing with invalid token
export should_contain=""
export should_return="70"

info 'Testing dataset listing with invalid token'
run_test s3cmd -c s3cmd.invalid ls s3://

# Test s3cmd dataset file listing with valid token
# Note that this does not print the full dataset name if it is a url, due to
# how s3cmd treats slashes in bucket names.
export should_contain="s3://https:/dummy_data"
export should_return="0"

info 'Testing dataset file listing with valid token'
run_test s3cmd -c s3cmd.valid ls "s3://$dataset"

# Test s3cmd dataset file listing with invalid token
export should_contain=""
export should_return="70"

info 'Testing dataset file listing with invalid token'
run_test s3cmd -c s3cmd.invalid ls s3://

# Test s3cmd dataset file listing with a filename prefix
# Note that this does not print the full dataset name if it is a url, due to
# how s3cmd treats slashes in bucket names.
export should_contain="s3://https:/dummy_data"
export should_return="0"

info 'Testing dataset listing with valid prefix and valid token'
run_test s3cmd -c s3cmd.valid ls "s3://$dataset/dummy"

export should_contain=""
info 'Testing dataset listing with invalid prefix and valid token'
run_test s3cmd -c s3cmd.valid ls "s3://$dataset/other"

export should_return="70"
info 'Testing dataset listing with valid prefix and invalid token'
run_test s3cmd -c s3cmd.invalid ls "s3://$dataset/dummy"

# Test s3cmd file download
export should_contain="download: 's3://$dataset/dummy_data' -> './dummy_data'"
export should_return="0"
info 'Testing valid file download with a valid token'
run_test s3cmd -c s3cmd.valid get "s3://$dataset/dummy_data"

export should_contain="06bb0a514b26497b4b41b30c547ad51d059d57fb7523eb3763cfc82fdb4d8fb7"
export should_return="0"
info 'Checking downloaded file sha256 checksum'
run_test sha256sum dummy_data | cut -d' ' -f 1

export should_contain=""
export should_return="70"
info 'Testing valid file download with an invalid token'
run_test s3cmd -c s3cmd.invalid get --force "s3://$dataset/dummy_data"


echo
info " ----- End of S3 Tests ------------------------------------------------- "
echo
