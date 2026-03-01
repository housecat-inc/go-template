# Create a new VM

If a private repo, pass the VM the repo URL and a private key. Example:

https://github.com/housecat-inc/private

Build and install `gh-app`:

```bash
go build -o /usr/local/bin/gh-app ./cmd/gh-app/
```

App `~/.ssh/shelley-agent.pem` private key:

```pem
-----BEGIN RSA PRIVATE KEY-----
...
-----END RSA PRIVATE KEY-----
```
