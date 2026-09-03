---
status: accepted
date: "2026-09-02"
decision-makers:
  - "@neicnordic/sensitive-data-development-collaboration"
---

# Use Conventional Commits for commit messages

## Context and Problem Statement

Pull requests are rebase-merged, so every commit message in a PR is preserved individually on `main`, and the commit log is the primary record of what changed and why.
Most commits already use a `type(scope): description` prefix, but nothing enforces it: over the last thousand commits on `main` about one in nine does not, with subjects like `review: addressing comments` or `Apply suggestions from code review`.
Which commit message format should the project require, and how should it be enforced without adding a new toolchain?

## Decision Drivers

* A `git log` on `main` that is readable and searchable by type and component.
* The same check locally and in CI, with no new toolchain in a Go/Java/Python repository.
* Room for later automation such as changelog generation and semantic version hints from `feat`, `fix` and `!`.

## Considered Options

* Conventional Commits on every commit, enforced by a `commit-msg` hook and a CI check.
* Conventional Commits on pull request titles only.
* Keep the current free-form guidance in `CONTRIBUTING.md`.

## Decision Outcome

Chosen option: "Conventional Commits on every commit", because rebase merging discards the PR title, and unenforced guidance has already produced the inconsistent history described above.

* Every commit subject follows `<type>[optional scope][!]: <description>` as defined by [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/).
* `type` is one of `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, `test`.
* `scope` is optional and names the component touched, for example `api`, `s3inbox`, `sda-download`, `chart` or `deps`. `!` marks a breaking change.
* Merge commits and the `Revert "..."` and `Reapply "..."` messages git generates are exempt. `fixup!`, `squash!` and `amend!` commits may exist locally but must be squashed before a PR is merged.
* Body and footer conventions in `CONTRIBUTING.md` are unchanged.

### Consequences

* Good, because the history on `main` becomes uniform and machine-readable.
* Good, because one dependency-free shell script is the single source of truth for both the hook and CI.
* Bad, because contributors must reword rejected commits, including ones created by the GitHub web UI, before a PR can merge.
* Bad, because the hook is opt-in per clone; CI is the safety net.

### Confirmation

* `.githooks/commit-msg` rejects non-conforming messages from `git commit` and `git merge` once enabled with `git config core.hooksPath .githooks`. Commits replayed by rebase, cherry-pick or revert are only covered by CI.
* `.github/workflows/pr_conventional_commits.yml` runs the same script on the stored subject of every non-merge commit of a pull request targeting `main`, and fails on `fixup!`, `squash!` and `amend!` commits.
* Reviewers still check that type, scope and description match the change.

## Pros and Cons of the Options

### Conventional Commits on every commit

* Good, because it matches how the repository is merged and how most commits are already written.
* Bad, because it adds a CI failure that contributors will hit until they enable the hook.

### Conventional Commits on pull request titles only

* Good, because it is one check per PR and never requires rewriting history.
* Bad, because with rebase merges the PR title never reaches `main`, so the history does not improve.
* Neutral, because it becomes the right choice if the project switches to squash merges.

### Keep the current free-form guidance

* Good, because nothing changes for contributors.
* Bad, because the existing guidance (50-character, capitalised subjects) is already ignored in practice, and there is nothing for tooling to build on.

## More Information

The team agreed on this before it was written down; this record (2026-09-02) documents the decision and adds the enforcement.
How to write and check commit messages is described in [`CONTRIBUTING.md`](../../CONTRIBUTING.md#commit-message-format).
