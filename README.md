# Housecat Auth

Authentication service for Housecat. Supports Google OAuth and exe.dev proxy authentication.

## Building and Running

Build with `make build`, then run `./bin/srv`. The server listens on port 8000 by default.

## Running on exe.dev

### First-time setup

Clone the repo:

```bash
git clone https://github.com/housecat-inc/auth
cd auth
```

Configure environment:

```bash
# Copy env template and fill in values
sudo cp .env.example /opt/srv/data/.env
sudo chmod 600 /opt/srv/data/.env
# Edit /opt/srv/data/.env with GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET
```

Install dependencies:

```bash
# Install dependencies
curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v4.2.1/tailwindcss-linux-x64 && chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss

# Install service
make install
sudo cp srv.service /etc/systemd/system/srv.service
sudo systemctl daemon-reload
sudo systemctl enable srv.service
sudo systemctl start srv

# Check status
systemctl status srv

# View logs
journalctl -u srv -f
```

### Redeploying after code changes

```bash
make install
sudo systemctl restart srv
```

### Systemd hardening

The production layout separates data `/opt/srv/data` (exedev 0700) from the binary `/opt/srv/bin/srv` (root 0755).

The service file includes:

- **ProtectHome=true** — `/home` is inaccessible to the service
- **ProtectSystem=strict** — the entire filesystem is read-only except explicit paths
- **ReadWritePaths=/opt/srv/data** — only the data directory is writable
- **NoNewPrivileges=true** — prevents privilege escalation
- **PrivateTmp=true** — isolated `/tmp`

## OAuth Apps

### Google

- [Dev / Staging](https://console.cloud.google.com/auth/clients/776457397167-pln4fpcftdtgh8lc5g0gh0s0mimb35s9.apps.googleusercontent.com?project=housecat-staging-v0)
- Prod: https://console.cloud.google.com/auth/clients?project=housecat-476300

Data Access

- [Gmail API](https://console.cloud.google.com/apis/library/gmail.googleapis.com?project=housecat-staging-v0)
  - auth/gmail.readonly
  - auth/gmail.compose
  - auth/gmail.send
  - auth/gmail.labels
  - auth/gmail.modify
- [GCal API](https://console.cloud.google.com/apis/library/calendar-json.googleapis.com?project=housecat-staging-v0)
  - auth/calendar.events.readonly (read level)
  - auth/calendar.events.owned (draft level)
  - auth/calendar.events (write/archive level)
  - auth/calendar.settings.readonly (read/draft/write — timezone detection)
- [GDrive API](https://console.cloud.google.com/apis/library/drive.googleapis.com?project=housecat-staging-v0)
  - auth/drive.readonly
  - auth/drive.file
  - auth/drive
- [GDocs API](https://console.cloud.google.com/apis/library/docs.googleapis.com?project=housecat-staging-v0)
  - auth/documents.readonly
  - auth/documents
  - auth/drive.readonly (read level — discover docs via Drive)
  - auth/drive.file (draft level — delete app-created docs)
  - auth/drive (archive level — deletion via Drive API)
- [GSheets API](https://console.cloud.google.com/apis/library/sheets.googleapis.com?project=housecat-staging-v0)
  - auth/spreadsheets.readonly
  - auth/spreadsheets
  - auth/drive.readonly (read level — discover spreadsheets via Drive)
  - auth/drive.file (draft level — delete app-created spreadsheets)
  - auth/drive (archive level — deletion via Drive API)

### Slack

- Prod: https://app.slack.com/app-settings/T0A49ED8YQY/A0A3243PSUF/oauth

### Notion

- Dev: https://www.notion.so/profile/integrations/public/30dd872b-594c-81d0-a1dc-00378bb7a079
- Prod: https://www.notion.so/profile/integrations/public/2c4d872b-594c-80ef-aed4-00379aff0462

## Connection Levels

Each integration supports multiple connection levels that control what actions are allowed. Levels are independent — connecting at one level does not grant access to another.

| Level   | Purpose                          |
|---------|----------------------------------|
| read    | Fetch and search data            |
| draft   | Create items for review          |
| write   | Create, send, and share          |
| archive | Trash and delete                 |

The `read → draft → write` levels form a fallback chain: a tool requiring `read` will accept a `draft` or `write` token. The `archive` level is **independent** — it never falls back to or from other levels.

### Archive level

Archive connections grant destructive permissions (trash, delete) separately from write access. This prevents accidental deletions when only write access was intended.

Services with archive support:

| Service | Archive scopes | Tool |
|---------|---------------|------|
| Gmail   | `gmail.modify` | `gmail_trash_message` |
| GCal    | `calendar` | `gcal_delete_event` |
| GDrive  | `drive` | `gdrive_trash_file` |
| GDocs   | `documents` + `drive` | `gdocs_delete_document` |
| GSheets | `spreadsheets` + `drive` | `gsheets_delete_spreadsheet` |

GDocs and GSheets archive levels request the `drive` scope in addition to their native scope because deletion goes through the Drive API.

## Authorization

To make public go to https://auth.housecat.com/admin and generate a setup token and pass the instructions to Shelley.

Then:

```bash
ssh exe.dev share set-public daemon-juliet
```

As a fallback exe.dev provides authorization headers like `X-ExeDev-UserID` and `X-ExeDev-Email` that are used to create a session.

## Git Proxy

A reverse proxy mounted on the main server at `/github.com/*` and `/api.github.com/*`. It injects GitHub App installation tokens and enforces per-client repo/branch policies. VMs use it instead of holding the GitHub App PEM directly.

### Setup

1. Copy the GitHub App PEM to the data directory:

```bash
sudo cp shelley-agent.pem /opt/srv/data/gh-app.pem
sudo chown exedev:exedev /opt/srv/data/gh-app.pem
sudo chmod 600 /opt/srv/data/gh-app.pem
```

2. Add to `/opt/srv/data/.env`:

```bash
GH_APP_PEM_PATH=gh-app.pem
GH_APP_ID=2976885
GH_INSTALLATION_ID=113185174
GH_ALLOWED_REPOS=housecat-inc/go-template,housecat-inc/app
```

The installation ID identifies which GitHub org/account the app is installed on. Find it with `gh api /app/installations --jq '.[0].id'`.

3. Restart the service:

```bash
sudo systemctl restart srv
```

### VM enrollment

When a new VM registers (`cmd/register` in `go-template`), it automatically:

1. Requests the `git` OIDC scope during client registration
2. Configures `git url.*.insteadOf` to route `github.com` through the proxy
3. Embeds OIDC client credentials in the proxy URL for Basic auth

### Policy

The proxy enforces:
- **Fetch/clone**: allowed for repos in `GH_ALLOWED_REPOS`
- **Push**: allowed for repos in the allowlist AND branches matching the client's prefix
- **API reads/writes**: allowed for repos in the allowlist

### Per-client branch prefixes

When a VM registers with the `git` scope, its branch prefix is `{vm-name}/*`. For example, VM `whale-orange` can only push to `whale-orange/*` branches. Unauthenticated push requests receive a 401 challenge; pushes to disallowed branches get a 403 with the allowed prefixes.

Policies are derived from the `oidc_clients` table — clients with the `git` scope authenticate via Basic auth (client_id:client_secret) embedded in the proxy URL.

## Database

This template uses sqlite (`db.sqlite3`). SQL queries are managed with `go tool sqlc`.

## UI

This template uses templ and templui. Run `go tool templ generate` to generate templates. Run `go tool templui` to list and install components. Run `git clone https://github.com/housecat-inc/templui-pro` to explore additional application building blocks.

## Code layout

- `cmd/srv`: main package (binary entrypoint)
- `db`: SQLite open + migrations
- `srv`: HTTP server logic (handlers, auth)
- `ui`: templ UI components
