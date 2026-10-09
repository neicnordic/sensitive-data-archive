# Agent instructions

This file is for AI coding agents (Claude Code reads it through `CLAUDE.md`; Codex and others read it directly) and for anyone who wants the short version.
[CONTRIBUTING.md](CONTRIBUTING.md), [DEVELOPMENT.md](DEVELOPMENT.md) and the [decision records](docs/decisions/README.md) have the details.

## Repository layout

* There is no root Go module: `sda/`, `sda-download/`, `sda-admin/` and `sda-validator/orchestrator/` each have their own `go.mod`, so run Go commands inside the module.
* `sda/cmd/download/` is download v2 and replaces the v1 service in `sda-download/`.

## Build, test and lint

* Use a Go version that meets each module's `go.mod`; `make go-version-check` only checks the coarser version in `sda/Dockerfile`.
* Unit tests per module: `make test-sda`, `make test-sda-download`, `make test-sda-admin` (each runs `go test ./... -count=1`), and `make test-sda-sftp-inbox` for the Java inbox.
  For the validator orchestrator, run `go test ./...` in `sda-validator/orchestrator/`.
  The full `sda-doa` (Java) suite needs its integration stack: `make integrationtest-sda-doa-posix` or `make integrationtest-sda-doa-s3`.
* Some `sda` packages start containers from `TestMain` and need a running Docker daemon: `internal/database/postgres` (PostgreSQL), `internal/broker` and `cmd/syncapi` (RabbitMQ), and `internal/userauth` (a Python mock OIDC server).
  `make test-sda-sftp-inbox` runs Maven in Docker too.
* CI also runs the download service with the `visas` build tag: `go test -tags=visas ./cmd/download/...` in `sda/`.
* Lint: `make lint-sda`, `make lint-sda-download`, `make lint-sda-admin`, and `golangci-lint run --timeout 5m` in `sda-validator/orchestrator/`.
  The root `.golangci.yml` is a golangci-lint v2 configuration, so v1 does not work.
* Integration tests (`make integrationtest-*`) run Docker Compose stacks and are slow.
  Not every target builds its images first: run `make build-all` before `make integrationtest-sda-download-v2`.
  CI runs them on PRs that touch the paths listed in `.github/workflows/pr_build_and_test_images.yml`.
* Markdown under `docs/decisions/` and `docs/rfcs/` is linted with markdownlint-cli2 using `.markdownlint.yml`.

## Conventions

* Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) ([ADR-0007](docs/decisions/0007-use-conventional-commits.md)).
  Enable the check once per clone with `git config core.hooksPath .githooks`; CI checks every commit in a PR as well.
* Use an imperative subject of about 70 characters with no trailing period.
  Add a body only when the reason for the change is not obvious from the diff, and wrap it at 72 characters.
* Branch names start with the Conventional Commits type, for example `fix/short-description`.
* PRs are rebase-merged through the merge queue, so every commit lands on `main`.
  Keep one logical change per commit, stage files explicitly (never `git add .`), and reword commits such as `fix review comments` before merge.
* Update a branch with `git rebase origin/main`; never merge `main` into it.
* `sda/CHANGELOG.md`, `sda-admin/CHANGELOG.md` and `charts/sda-svc/CHANGELOG.md` follow Keep a Changelog; changes that users or operators notice get an entry under `[Unreleased]`.
* New Go code logs with `log/slog` ([ADR-0009](docs/decisions/0009-use-log-slog-for-go-logging.md)).
  A service switches from logrus in a single PR, so when you edit a service that still uses logrus, keep logrus there.
  When a service is migrated, `Fatal` calls become errors returned to `main`, and audit output such as the download audit log stays separate from application logging.
* Use `accessionID` in Go code and API messages and `accessionId` in Java; database columns, views, functions and SQL strings keep `stable_id` ([ADR-0008](docs/decisions/0008-use-accessionid-in-application-code.md)).
* Service documentation such as `sda/cmd/download/download.md` is copied into the [neic-sda documentation site](https://github.com/neicnordic/neic-sda).
  Fix documentation here, not on the site, and use absolute GitHub URLs when linking to files that are not copied as well.
* Code, comments, documentation, commit messages and PR text are in English.
* Write PR and issue text without hard line breaks: one line per paragraph or list item.
* This is a public repository: do not reference private repositories or internal tracker numbers.

## Working principles

* Make the simplest change that solves the stated problem.
  If it could be much shorter, rewrite it; ask whether a senior engineer would call it overcomplicated.
* Keep the scope tight: every changed line should trace back to the issue.
  Mention unrelated bugs or dead code in the PR description instead of fixing them in the same change.
* Match the surrounding style, even where you would do it differently.
  Remove imports, variables and functions that your change left unused, and leave existing dead code alone.
* State your assumptions explicitly.
  Ask before guessing when a wrong guess would be costly to undo, such as data loss, schema or API changes, or production impact.
* Define done as something you can verify: a bug fix starts with a test that reproduces the bug, and a refactor keeps the tests green before and after.

## Related repositories

These public repositories depend on this one or document it, so changes to APIs, configuration or documentation can affect them.

| Repository | Relationship |
| --- | --- |
| [NBISweden/sda-cli](https://github.com/NBISweden/sda-cli) | Command-line client for the inbox and download APIs |
| [NBISweden/sda-download-ui](https://github.com/NBISweden/sda-download-ui) | Web interface for download v2 |
| [neicnordic/neic-sda](https://github.com/neicnordic/neic-sda) | Documentation site that copies the service documentation from this repository |
| [neicnordic/crypt4gh](https://github.com/neicnordic/crypt4gh) | Crypt4GH Go library used by the services |
| [umccr/htsget-rs](https://github.com/umccr/htsget-rs) | htsget server used alongside the download service |
| [GenomicDataInfrastructure/starter-kit-storage-and-interfaces](https://github.com/GenomicDataInfrastructure/starter-kit-storage-and-interfaces) | GDI starter kit that deploys the SDA |
