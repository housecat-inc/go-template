package gh

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
)

var allowedHosts = map[string]bool{
	"api.github.com": true,
	"github.com":     true,
}

// Proxy is a reverse proxy that forwards requests to GitHub,
// injecting GitHub App installation tokens and enforcing per-client policies.
//
// Requests arrive as: https://proxy-host:port/{github-host}/{path}
// e.g. https://proxy:8000/github.com/org/repo.git/info/refs
//
// Clients authenticate via Basic auth (client_id:client_secret).
type Proxy struct {
	DefaultPolicy *Policy
	HTTPClient    *http.Client
	PolicyStore   PolicyStore
	TokenSource   *TokenSource

	UpstreamScheme string
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, path := splitProxyPath(r.URL.Path)
	if host == "" || !allowedHosts[host] {
		http.Error(w, fmt.Sprintf("host %q not allowed", host), http.StatusForbidden)
		return
	}

	policy, clientName, authenticated := p.resolvePolicy(r)
	slog.Info("proxy request", "host", host, "method", r.Method, "path", path, "client", clientName)

	if !authenticated && isPushRequest(path, r.URL.Query().Get("service")) {
		slog.Info("push request without auth, sending 401 challenge", "path", path)
		w.Header().Set("WWW-Authenticate", `Basic realm="git proxy"`)
		http.Error(w, "authentication required for push", http.StatusUnauthorized)
		return
	}

	var bodyBuf bytes.Buffer
	if r.Body != nil {
		if _, err := io.Copy(&bodyBuf, r.Body); err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadGateway)
			return
		}
		r.Body.Close()
	}
	body := bodyBuf.Bytes()

	policyReq := &http.Request{Method: r.Method, URL: r.URL}
	policyReq.URL.Path = path
	if err := checkPolicy(policy, policyReq, body); err != nil {
		slog.Warn("policy denied", "method", r.Method, "path", path, "error", err)
		if isGitReceivePack(path) {
			refs, _ := ParseReceivePackRefs(body)
			rejectGitPush(w, refs, errors.UnwrapAll(err).Error())
		} else {
			http.Error(w, err.Error(), http.StatusForbidden)
		}
		return
	}

	token, err := p.TokenSource.Token(r.Context())
	if err != nil {
		http.Error(w, "get github token: "+err.Error(), http.StatusBadGateway)
		return
	}

	scheme := p.UpstreamScheme
	if scheme == "" {
		scheme = "https"
	}
	upstreamURL := scheme + "://" + host + path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "create upstream request: "+err.Error(), http.StatusBadGateway)
		return
	}

	copyHeaders(upReq.Header, r.Header)
	if isGitRequest(path) {
		upReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token)))
	} else {
		upReq.Header.Set("Authorization", "Bearer "+token)
	}
	upReq.Host = host

	slog.Info("proxy upstream", "method", r.Method, "url", upstreamURL)

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(upReq)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)

	slog.Info("proxy response", "status", resp.StatusCode, "url", upstreamURL)
}

func (p *Proxy) resolvePolicy(r *http.Request) (*Policy, string, bool) {
	creds := extractBasicAuth(r)
	if creds != "" && p.PolicyStore != nil {
		policy, err := p.PolicyStore.Lookup(r.Context(), creds)
		if err == nil && policy != nil {
			return policy, policyClientName(policy), true
		}
		slog.Warn("proxy auth failed, using default policy", "error", err)
	}
	return p.DefaultPolicy, "default", creds != ""
}

func extractBasicAuth(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	if strings.HasPrefix(auth, "Basic ") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if err != nil {
			return ""
		}
		return string(decoded)
	}
	return ""
}

func policyClientName(p *Policy) string {
	if len(p.BranchPrefixes) > 0 {
		return strings.TrimSuffix(p.BranchPrefixes[0], "/*")
	}
	return "unknown"
}

func splitProxyPath(urlPath string) (host, path string) {
	p := strings.TrimPrefix(urlPath, "/")
	slash := strings.Index(p, "/")
	if slash < 0 {
		return p, "/"
	}
	return p[:slash], "/" + p[slash+1:]
}

func checkPolicy(policy *Policy, req *http.Request, body []byte) error {
	urlPath := req.URL.Path

	if isGitReceivePack(urlPath) {
		repo := repoFromGitPath(urlPath)
		refs, err := ParseReceivePackRefs(body)
		if err != nil {
			return errors.Wrap(err, "parse receive-pack refs")
		}
		return policy.CheckPush(repo, refs)
	}

	if isGitUploadPack(urlPath) || isGitInfoRefs(urlPath) {
		repo := repoFromGitPath(urlPath)
		return policy.CheckFetch(repo)
	}

	return policy.CheckAPI(req.Method, urlPath)
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		switch strings.ToLower(k) {
		case "authorization", "host", "proxy-authorization", "proxy-connection":
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func isGitReceivePack(path string) bool {
	return strings.HasSuffix(path, "/git-receive-pack")
}

func isGitUploadPack(path string) bool {
	return strings.HasSuffix(path, "/git-upload-pack")
}

func isGitInfoRefs(path string) bool {
	return strings.HasSuffix(path, "/info/refs")
}

func isGitRequest(path string) bool {
	return isGitReceivePack(path) || isGitUploadPack(path) || isGitInfoRefs(path)
}

func rejectGitPush(w http.ResponseWriter, refs []RefUpdate, reason string) {
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(ReceivePackReject(refs, reason))
}

func isPushRequest(path, service string) bool {
	if isGitReceivePack(path) {
		return true
	}
	if isGitInfoRefs(path) && service == "git-receive-pack" {
		return true
	}
	return false
}

