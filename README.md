# Go Shelley Template

This is a starter template for building Go web applications for Housecat. It demonstrates end-to-end usage including HTTP handlers, authentication, database integration, and deployment.

## Building and Running

Build with `make build`, then run `./bin/srv`. The server listens on port 8000 by default.

## Running on exe.dev

Install dependencies:

```bash
curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v4.2.1/tailwindcss-linux-x64 && chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss
```

To run the server as a systemd service:

```bash
# Install the service file
sudo cp srv.service /etc/systemd/system/srv.service

# Reload systemd and enable the service
sudo systemctl daemon-reload
sudo systemctl enable srv.service

# Start the service
sudo systemctl start srv

# Check status
systemctl status srv

# View logs
journalctl -u srv -f
```

To restart after code changes:

```bash
make build
sudo systemctl restart srv
```

## Authorization

exe.dev provides authorization headers and login/logout links
that this template uses.

When proxied through exed, requests will include `X-ExeDev-UserID` and
`X-ExeDev-Email` if the user is authenticated via exe.dev.

To make public

```bash
ssh exe.dev share set-public daemon-juliet
```

## Database

This template uses sqlite (`db.sqlite3`). SQL queries are managed with sqlc.

## UI

This template uses templ and templui. Run `go tool templ generate` to generate templates. Run `go tool templui` to list and install components. 

## Code layout

- `cmd/srv`: main package (binary entrypoint)
- `srv`: HTTP server logic (handlers)
- `ui`: templ UI components
- `db`: SQLite open + migrations (001-base.sql)
