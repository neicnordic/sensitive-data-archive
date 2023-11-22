# linter modules to be included/excluded
LINT_INCLUDE=-E bodyclose,gocritic,gofmt,gosec,govet,nestif,nlreturn,revive,rowserrcheck
LINT_EXCLUDE=-e G401,G501,G107

help:
	@echo 'Welcome!'
	@echo ''
	@echo 'This Makefile is designed to make the development work go smoothly.'
	@echo 'Indepth description of how to use this Makefile can be fould in the README.md'

bootstrap: go-version-check
		@for dir in sda sda-auth sda-download; do \
			cd $$dir; \
			go get ./...; \
			cd ..; \
		done
		if ! command -v curl >/dev/null; then \
			echo "Can't install golangci-lint because curl is missing."; \
			exit 1; \
		fi
		@curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
		sh -s -- -b $$(go env GOPATH)/bin
		GO111MODULE=off go get golang.org/x/tools/cmd/goimports

# build containers
build-all: build-postgresql build-rabbitmq build-sda build-sda-auth build-sda-download build-sda-sftp-inbox
build-postgresql:
	@cd postgresql && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-postgres .
build-rabbitmq:
	@cd rabbitmq && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-rabbitmq .
build-sda:
	@cd sda && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F) .
build-sda-auth:
	@cd sda-auth && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-auth .
build-sda-download:
	@cd sda-download && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-download .
build-sda-sftp-inbox:
	@cd sda-sftp-inbox && docker build -t ghcr.io/neicnordic/sensitive-data-archive:PR$$(date +%F)-sftp-inbox .


go-version-check: SHELL:=/bin/bash
go-version-check:
	@GO_VERSION_MIN=$$(grep GOLANG_VERSION $(CURDIR)/sda/Dockerfile | cut -d '-' -f2 | tr -d '}'); \
	GO_VERSION=$$(go version | grep -o 'go[0-9]\+\.[0-9]\+\(\.[0-9]\+\)\?' | tr -d 'go'); \
	IFS="." read -r -a GO_VERSION_ARR <<< "$${GO_VERSION}"; \
	IFS="." read -r -a GO_VERSION_REQ <<< "$${GO_VERSION_MIN}"; \
	if [[ $${GO_VERSION_ARR[0]} -lt $${GO_VERSION_REQ[0]} ||\
		( $${GO_VERSION_ARR[0]} -eq $${GO_VERSION_REQ[0]} &&\
		( $${GO_VERSION_ARR[1]} -lt $${GO_VERSION_REQ[1]} ||\
		( $${GO_VERSION_ARR[1]} -eq $${GO_VERSION_REQ[1]} && $${GO_VERSION_ARR[2]} -lt $${GO_VERSION_REQ[2]} )))\
	]]; then\
		echo "SDA requires go $${GO_VERSION_MIN} to build; found $${GO_VERSION}.";\
		exit 1;\
	fi;


# run intrgration tests, same as being run in Github Actions during a PR
integrationtest-postgres: build-postgresql
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/postgres.yml run tests
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/postgres.yml down -v --remove-orphans
integrationtest-rabbitmq: build-rabbitmq build-sda
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/rabbitmq-federation.yml run federation_test
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/rabbitmq-federation.yml down -v --remove-orphans
integrationtest-sda: build-all
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml run integration_tests
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-s3-integration.yml down -v --remove-orphans
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml run integration_tests
	@PR_NUMBER=$$(date +%F) docker compose -f .github/integration/sda-posix-integration.yml down -v --remove-orphans

# lint go code
lint-all: lint-sda lint-sda-auth lint-sda-download
lint-sda:
	@echo 'Running golangci-lint in the `sda` folder'
	@cd sda && golangci-lint run $(LINT_INCLUDE) $(LINT_EXCLUDE)
lint-sda-auth:
	@echo 'Running golangci-lint in the `sda-auth` folder'
	@cd sda-auth && golangci-lint run $(LINT_INCLUDE) $(LINT_EXCLUDE)
lint-sda-download:
	@echo 'Running golangci-lint in the `sda-download` folder'
	@cd sda-download && golangci-lint run $(LINT_INCLUDE) $(LINT_EXCLUDE)

# run static code tests
test-all: test-sda test-sda-auth test-sda-download test-sda-sftp-inbox
test-sda:
	@cd sda && go test ./... -count=1
test-sda-auth:
	@cd sda-auth && go test ./... -count=1
test-sda-download:
	@cd sda-download && go test ./... -count=1
test-sda-sftp-inbox:
	@docker run --rm -v ./sda-sftp-inbox:/inbox maven:3.9.4-eclipse-temurin-21-alpine sh -c "cd /inbox && mvn test -B"
