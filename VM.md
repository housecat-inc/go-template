# Create a new VM

If a private repo, pass the VM the repo URL and a private key. Example:

https://github.com/housecat-inc/private

Install `gh-app`:

```bash
go install srv.housecat.com/cmd/gh-app@latest
```

App `~/.ssh/shelley-agent.pem` private key:

```pem
-----BEGIN RSA PRIVATE KEY-----
...
-----END RSA PRIVATE KEY-----
```
