# Go Shelley Template

This is a starter template for building Go web applications for Housecat. It demonstrates end-to-end usage including HTTP handlers, authentication, database integration, and deployment.

## Building and Running

Build with `make build`, then run `./bin/srv`. The server listens on port 8000 by default.

## Running on exe.dev

Install dependencies:

```bash
curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v4.2.1/tailwindcss-linux-x64 && chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss
```

### Directory layout

The production layout separates the binary from its data:

```
/opt/srv/
├── bin/srv          # binary (owned root:root, 0755)
└── data/            # working directory (owned exedev:exedev, 0700)
    ├── db.sqlite3   # database
    └── .env         # environment (optional, 0600)
```

The service binary is read-only to the app process. Only `/opt/srv/data` is writable, so a vulnerability in the web app cannot modify the binary, read the home directory, or write elsewhere on the filesystem.

### First-time setup

```bash
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

As a fallback exe.dev provides authorization headers and login/logout links. When proxied through exed, requests will include `X-ExeDev-UserID` and `X-ExeDev-Email` if the user is authenticated via exe.dev.

## Database

This template uses sqlite (`db.sqlite3`) stored in `/opt/srv/data/`. SQL queries are managed with sqlc.

## UI

This template uses templ and templui. Run `go tool templ generate` to generate templates. Run `go tool templui` to list and install components.

## Code layout

- `cmd/srv`: main package (binary entrypoint)
- `db`: SQLite open + migrations (001-base.sql)
- `srv`: HTTP server logic (handlers)
- `ui`: templ UI components
