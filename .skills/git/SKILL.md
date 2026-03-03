---
name: git
description: Git and GitHub workflow — auth via GitHub App, branch management for feature work, opening pull requests, and syncing with main.
---

## Access

```bash
# view git proxy and user config
git config --global --list

# verify basic git access
git ls-remote --heads https://github.com/housecat-inc/go-template.git

# verify basic gh cli access
gh auth status
```

## Branch Management

### Starting feature work

Always branch with a `$HOSTNAME/` from an up-to-date `main`:

```bash
git checkout main
git pull origin main
git checkout -b $HOSTNAME/short-description
```

Use descriptive branch names: `warm-bengal/feature-add-search`, `warm-bengal/fix-login-redirect`, `warm-bengal/refactor-db-layer`.

Prefer rebasing on main for a clean history.

### Committing

Make small, focused commits with clear messages:

```bash
git add -A
git commit -m "add search endpoint and handler"
```

### Pushing

```bash
git push origin $HOSTNAME/short-description
```

## Opening Pull Requests

### PR title and description

Use `gh` to open GitHub pull requests with a title and description as customer-facing release notes. Focus on what changed for the user, not code details, test coverage, or file-level changes. The code reviewer will read the code and run automated tests.

```bash
gh auth login
gh pr create --title "Added GitHub integration" --body "- Add GitHub app sign in option
- Add GitHub project management service integration
- Add GitHub flavored markdown (GFM) to document renderer"
```

**Title:** Past-tense summary of the user-visible change.

**Body:** Bullet list of notable changes.

Example:

```
Title: Added GitHub integration
Body:
- Add GitHub app sign in option
- Add GitHub project management service integration
- Add GitHub flavored markdown (GFM) to document renderer
```
