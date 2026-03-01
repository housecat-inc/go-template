---
name: git
description: Git and GitHub workflow — authentication via GitHub App, branch management for feature work, opening pull requests, and syncing with main.
---

# Git & GitHub Workflow

## Authentication

This VM authenticates to GitHub as `shelley-agent[bot]` using a GitHub App (ID: `2976885`).

### How it works

- A private key is stored at `~/.ssh/shelley-agent.pem`
- `.skills/git/gh-app` generates a JWT from the key, exchanges it for a short-lived installation token (1 hour), then runs `gh`
- Config is via env vars:
  - `GH_APP_ID` (default: `2976885`)
  - `GH_APP_PEM` (default: `~/.ssh/shelley-agent.pem`)
  - `GH_APP_REPO` (optional, scopes token to a single repo name e.g. `go-template`)
- Tokens are independent — multiple VMs can use the same PEM without conflicts

### Setup

Install `gh-app` to PATH (once per VM):

```bash
sudo ln -sf "$(pwd)/.skills/git/gh-app" /usr/local/bin/gh-app
```

### Commands

- **`gh-app <args>`** — refreshes the token on-demand, then runs `gh`. Use this for GitHub operations. Adds ~1 second overhead.
- **`gh <args>`** — uses the cached token. Works until the token expires (~1 hour).

### Refreshing

Tokens are refreshed on-demand, not on a schedule. Refresh once at the start of a session:

```bash
gh-app auth status
```

Or before any `gh` command by using `gh-app` instead of `gh`.

### Git credential helper

After authenticating, configure git to use `gh` for HTTPS credentials:

```bash
gh auth setup-git
```

This only needs to be run once per VM. Without it, `git push` over HTTPS will fail.

### Git identity

```
git config --global user.name  = shelley-agent[bot]
git config --global user.email = 2976885+shelley-agent[bot]@users.noreply.github.com
```

## Branch Management

### Starting feature work

Always branch from an up-to-date `main`:

```bash
git checkout main
git pull origin main
git checkout -b feature/short-description
```

Use descriptive branch names: `feature/add-search`, `fix/login-redirect`, `refactor/db-layer`.

### Committing

Make small, focused commits with clear messages:

```bash
git add -A
git commit -m "add search endpoint and handler"
```

### Pushing

```bash
gh-app auth status  # ensure token is fresh
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

### Creating PRs

```bash
gh-app pr create --title "Added search" --body "- Add full-text search to documents" --base main
```

For draft PRs:

```bash
gh-app pr create --title "Added search" --body "..." --base main --draft
```

### Updating PR title/description

```bash
gh-app pr edit --title "Updated title" --body "- Updated description"
```

### Checking PR status

```bash
gh-app pr status
gh-app pr view
```

## Syncing with Main

### Update your feature branch with latest main

Prefer rebase for a clean history:

```bash
git fetch origin
git rebase origin/main
```

If there are conflicts, resolve them, then:

```bash
git add -A
git rebase --continue
```

After rebasing, force-push the feature branch:

```bash
git push origin feature/short-description --force-with-lease
```

### After a PR is merged

Return to main and pull:

```bash
git checkout main
git pull origin main
```

Clean up the merged branch:

```bash
git branch -d feature/short-description
```

## Quick Reference

| Task | Command |
|---|---|
| Refresh auth | `gh-app auth status` |
| New branch | `git checkout -b feature/name` |
| Push branch | `git push origin feature/name` |
| Open PR | `gh-app pr create --title "Added ..." --body "- ..." --base main` |
| Edit PR | `gh-app pr edit --title "..." --body "..."` |
| Sync with main | `git fetch origin && git rebase origin/main` |
| Check PR status | `gh-app pr status` |
| Merge PR | `gh-app pr merge --squash` |
| Clean up | `git checkout main && git pull && git branch -d feature/name` |
