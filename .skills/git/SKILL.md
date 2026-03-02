---
name: git
description: Git and GitHub workflow — auth via GitHub App, branch management for feature work, opening pull requests, and syncing with main.
---

## Auth

The `gh` wrapper (`cmd/gh`) transparently refreshes GitHub tokens before running the real `gh` CLI. It supports two modes:

- **Proxy mode** (`GH_PROXY_URL` set): fetches a token from the auth proxy
- **Direct mode** (PEM at `~/.ssh/shelley-agent.pem`): authenticates directly with GitHub

### VM Auth Setup

Install the wrapper and configure git (once per VM):

```bash
# write ~/.ssh/shelley-agent.pem
go install github.com/housecat-inc/go-template/cmd/gh@latest
gh auth setup-git
git config --global user.name  = shelley-agent[bot]
git config --global user.email = 2976885+shelley-agent[bot]@users.noreply.github.com
```

The wrapper installs to `~/go/bin/gh` and finds the real `gh` at `/usr/bin/gh` automatically.

## Branch Management

### Starting feature work

Always branch from an up-to-date `main`:

```bash
git checkout main
git pull origin main
git checkout -b feature/short-description
```

Use descriptive branch names: `feature/add-search`, `fix/login-redirect`, `refactor/db-layer`.

Prefer rebasing on main for a clean history.

### Committing

Make small, focused commits with clear messages:

```bash
git add -A
git commit -m "add search endpoint and handler"
```

### Pushing

```bash
gh auth status
git push origin feature/short-description
```

## Opening Pull Requests

### PR title and description

Write titles and descriptions as customer-facing release notes. Focus on what changed for the user, not implementation details. The code reviewer will read the code and run automated tests.

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

Do NOT describe code details, test coverage, refactoring mechanics, or file-level changes.
