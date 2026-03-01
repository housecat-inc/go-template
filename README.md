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

## Authorization

To make public go to https://auth.housecat.com/admin and generate a setup token and pass the instructions to Shelley.

Then:

```bash
ssh exe.dev share set-public daemon-juliet
```

As a fallback exe.dev provides authorization headers like `X-ExeDev-UserID` and `X-ExeDev-Email` that are used to create a session.

## Database

This template uses sqlite (`db.sqlite3`). SQL queries are managed with `go tool sqlc`.

## UI

This template uses templ and templui. Run `go tool templ generate` to generate templates. Run `go tool templui` to list and install components. Run `git clone https://github.com/housecat-inc/templui-pro` to explore additional application building blocks.

## Code layout

- `cmd/srv`: main package (binary entrypoint)
- `db`: SQLite open + migrations
- `srv`: HTTP server logic (handlers, auth)
- `ui`: templ UI components
