# Create a new VM

Clone https://github.com/housecat-inc/auth to auth.

For private repos install `gh-app`:

```bash
go install github.com/housecat-inc/go-template/cmd/gh-app@latest
```

And a GitHub App private key to `~/.ssh/shelley-agent.pem`:

```pem
-----BEGIN RSA PRIVATE KEY-----
...
-----END RSA PRIVATE KEY-----
```
