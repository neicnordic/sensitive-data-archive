# Contributing Guidelines

We thank you in advance 👍 🎉 for taking the time to contribute, whether with *code* or with *ideas*, to the NeIC SDA project.

## Did you find a bug?

- Ensure that the bug has not already been reported by [searching under Issues].

- If you're unable to find an issue addressing the problem, open a new one by using the following [template to report a bug]. Be sure to include:

  - a *clear* description,
  - as much relevant information as possible, and
  - a *code sample* or an (executable) *test case* demonstrating the expected behaviour that is not occurring.

- If possible, prefix the issue title with a short identifier for the relevant repository component, e.g. **[ingest]**, **[postgres]** etc.

## How to work on a new feature/bug

- Create an issue on Github or you can alternatively pick one already created.

- Assign yourself to that issue.

- Discussions on how to proceed about that issue take place in the comment section on that issue.

To avoid unnecessary work duplication and waste of time and effort, it's generally a good idea to discuss the issue beforehand with the team. Some of the work might have been done already by a co-worker. Also, as some of the features might impact different components, please communicate the intended changes, so that planning can be done accordingly.

## How we work with Git

All work takes place in feature branches. Give your branch a short descriptive name and prefix the name with the most suitable of:

- `feature/`
- `docs/`
- `bugfix/`
- `test/`
- `refactor/`

Use comments in your code, choose variable and function names that clearly show what you intend to implement.

Once the feature is done you can request it to be merged back into `main` by making a Pull Request.

Before making the pull request, it is a good idea to rebase your branch onto `main` to ensure that eventual conflicts with the `main` branch are solved before the PR is reviewed and that there can be a clean merge.
> NOTE:
> In older github repositories the default branch might be called `master` instead of `main`.

### About git and commit messages

In general it is better to commit often. Small commits are easier to roll back and also make the code easier to review.

Write helpful commit messages that describe the changes and possibly why they were necessary.

Each commit should contain changes that are functionally connected and/or related. If, for example, the first line of the commit message contains the word *and*, this is an indicator that this commit should have been split into two.

> NOTE:
> The commands `git add -p` or `git commit -p` can prove useful for selecting chunks of changed files in order to group unrelated things into multiple separate commits.

#### Helpful commit messages

The commit messages may be seen as meta-comments on the code that are incredibly helpful for anyone who wants to know how this piece of software is working, including colleagues (current and future) and external users.

Some tips about writing helpful commit messages:

 1. Separate subject (the first line of the message) from body with a  blank line.
 2. Limit the subject line to 50 characters.
 3. Capitalize the subject line.
 4. Do not end the subject line with a period.
 5. Use the imperative mood in the subject line.
 6. Wrap the body at 72 characters.
 7. Use the body to explain what and why vs. how.

For an in-depth explanation of the above points, please see [How to Write a Git Commit Message](https://chris.beams.io/posts/git-commit/).

### How we do code reviews

A code review is initiated when someone has made a Pull Request in the appropriate repo on github.

Work should not continue on the branch *unless* it is a [Draft Pull Request](https://github.blog/2019-02-14-introducing-draft-pull-requests/). Once the PR is marked ready the review can start.

The initiator of the PR should assign the [@sensitive-data-development-collaboration](https://github.com/orgs/neicnordic/teams/sensitive-data-development-collaboration) team as reviewers.

Other people may also look at the PR and review the code.

A reviewer's job is to:

- Write polite and friendly comments - remember that it can be tough to have other people criticizing your work, a little kindness goes a long way. This does not mean we should not comment on things that need to be changed of course, but instead focus on providing constructive criticism on how the work can be improved.
- Read the code and make sure it is understandable
- Make sure that commit messages and commits are structured so that it is possible to understand why certain changes were made.
- Ensure that the test-suite covers the new behavior

It is *not* the reviewer's job to checkout and run the code - that is what the test-suite is for.

Once at least 3 reviews from 3 different partners are positive, the Pull Request can be *merged* into `main` and the feature branch deleted.

If it takes long for some partner to review code, it is common practice to try to contact them on slack to see what the problem is and if it can be resolved quickly or whether it's ok to merge.

----

Thanks again,
/NeIC System Developers

[searching under Issues](https://github.com/neicnordic/sensitive-data-archive/issues?utf8=%E2%9C%93&q=is%3Aissue%20label%3Abug)
[template to report a bug](https://github.com/neicnordic/sensitive-data-archive/issues/new?assignees=&labels=bug&projects=&template=BUG_REPORT.md)
[open Issues](https://github.com/neicnordic/sensitive-data-archive/issues/?q=is%3Aissue%20state%3Aopen)
[template to open a new Pull Request](https://github.com/neicnordic/sensitive-data-archive/blob/main/.github/PULL_REQUEST_TEMPLATE.md)
