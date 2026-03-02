package gh

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProxy builds a Proxy wired to a fake upstream and optional policy store.
func testProxy(t *testing.T, upstream *httptest.Server, store PolicyStore) (*Proxy, *httptest.Server) {
	t.Helper()

	upstreamURL, _ := url.Parse(upstream.URL)

	oldHosts := allowedHosts
	allowedHosts = map[string]bool{"github.com": true, "api.github.com": true}
	t.Cleanup(func() { allowedHosts = oldHosts })

	p := &Proxy{
		DefaultPolicy: &Policy{
			AllowedOps:   []Op{OpFetch, OpAPIRead},
			AllowedRepos: []string{"org/repo", "org/other"},
		},
		HTTPClient:     &http.Client{Transport: &rewriteTransport{target: upstreamURL.Host}},
		PolicyStore:    store,
		TokenSource:    &TokenSource{token: "test-gh-token", exp: farFuture()},
		UpstreamScheme: "http",
	}

	ps := httptest.NewServer(p)
	t.Cleanup(ps.Close)
	return p, ps
}

func TestSplitProxyPath(t *testing.T) {
	a := assert.New(t)

	host, path := splitProxyPath("/github.com/org/repo.git/info/refs")
	a.Equal("github.com", host)
	a.Equal("/org/repo.git/info/refs", path)

	host, path = splitProxyPath("/api.github.com/repos/org/repo")
	a.Equal("api.github.com", host)
	a.Equal("/repos/org/repo", path)

	host, path = splitProxyPath("/github.com")
	a.Equal("github.com", host)
	a.Equal("/", path)
}

func TestProxyRejectsDisallowedHost(t *testing.T) {
	a := assert.New(t)
	p := &Proxy{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/evil.com/foo", nil)
	p.ServeHTTP(rec, req)
	a.Equal(http.StatusForbidden, rec.Code)
}

// ---------------------------------------------------------------------------
// E2E: full git fetch flow (info/refs + upload-pack)
// ---------------------------------------------------------------------------

func TestE2E_FetchNoAuth(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	var mu sync.Mutex
	var reqs []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		reqs = append(reqs, req.Method+" "+req.URL.Path+"?"+req.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte("001e# service=git-upload-pack\n0000"))
	}))
	defer upstream.Close()

	_, ps := testProxy(t, upstream, nil)

	// Step 1: GET info/refs?service=git-upload-pack (no auth — fetch is allowed by default policy)
	resp, err := http.Get(ps.URL + "/github.com/org/repo.git/info/refs?service=git-upload-pack")
	r.NoError(err)
	defer resp.Body.Close()
	a.Equal(http.StatusOK, resp.StatusCode)

	// Step 2: POST git-upload-pack
	resp2, err := http.Post(ps.URL+"/github.com/org/repo.git/git-upload-pack", "application/x-git-upload-pack-request", bytes.NewReader([]byte("0000")))
	r.NoError(err)
	defer resp2.Body.Close()
	a.Equal(http.StatusOK, resp2.StatusCode)

	mu.Lock()
	a.Len(reqs, 2)
	mu.Unlock()
}

// ---------------------------------------------------------------------------
// E2E: push flow — 401 challenge when unauthenticated
// ---------------------------------------------------------------------------

func TestE2E_Push401ChallengeNoAuth(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Fatal("should not reach upstream for unauthenticated push")
	}))
	defer upstream.Close()

	_, ps := testProxy(t, upstream, nil)

	// info/refs?service=git-receive-pack without auth → 401
	resp, err := http.Get(ps.URL + "/github.com/org/repo.git/info/refs?service=git-receive-pack")
	r.NoError(err)
	defer resp.Body.Close()

	a.Equal(http.StatusUnauthorized, resp.StatusCode)
	a.Contains(resp.Header.Get("WWW-Authenticate"), "Basic")

	// POST git-receive-pack without auth → 401
	resp2, err := http.Post(ps.URL+"/github.com/org/repo.git/git-receive-pack", "application/x-git-receive-pack-request", nil)
	r.NoError(err)
	defer resp2.Body.Close()
	a.Equal(http.StatusUnauthorized, resp2.StatusCode)
}

// ---------------------------------------------------------------------------
// E2E: push flow — authenticated, allowed branch prefix
// ---------------------------------------------------------------------------

func TestE2E_PushAllowedBranch(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	var mu sync.Mutex
	var reqs []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		reqs = append(reqs, req.Method+" "+req.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := &staticPolicyStore{
		policies: map[string]*Policy{
			"cid:csecret": {
				AllowedOps:     []Op{OpFetch, OpPush},
				AllowedRepos:   []string{"org/repo"},
				BranchPrefixes: []string{"test-vm/*"},
			},
		},
	}

	_, ps := testProxy(t, upstream, store)
	client := &http.Client{}

	// Step 1: info/refs?service=git-receive-pack WITH auth → 200 (forwarded)
	req, _ := http.NewRequest("GET", ps.URL+"/github.com/org/repo.git/info/refs?service=git-receive-pack", nil)
	req.SetBasicAuth("cid", "csecret")
	resp, err := client.Do(req)
	r.NoError(err)
	defer resp.Body.Close()
	a.Equal(http.StatusOK, resp.StatusCode)

	// Step 2: POST git-receive-pack with valid ref
	pktBody := buildReceivePackBody("refs/heads/test-vm/my-feature")
	req2, _ := http.NewRequest("POST", ps.URL+"/github.com/org/repo.git/git-receive-pack", bytes.NewReader(pktBody))
	req2.SetBasicAuth("cid", "csecret")
	req2.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	resp2, err := client.Do(req2)
	r.NoError(err)
	defer resp2.Body.Close()
	a.Equal(http.StatusOK, resp2.StatusCode)

	mu.Lock()
	a.Len(reqs, 2)
	mu.Unlock()
}

// ---------------------------------------------------------------------------
// E2E: push flow — authenticated, disallowed branch prefix → 403 with hint
// ---------------------------------------------------------------------------

func TestE2E_PushDisallowedBranch(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// info/refs is allowed (it's a fetch check) but receive-pack should not arrive
		if strings.HasSuffix(req.URL.Path, "/git-receive-pack") {
			t.Fatal("disallowed push should not reach upstream")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := &staticPolicyStore{
		policies: map[string]*Policy{
			"cid:csecret": {
				AllowedOps:     []Op{OpFetch, OpPush},
				AllowedRepos:   []string{"org/repo"},
				BranchPrefixes: []string{"test-vm/*"},
			},
		},
	}

	_, ps := testProxy(t, upstream, store)
	client := &http.Client{}

	// Push to main → 200 with git protocol rejection
	pktBody := buildReceivePackBody("refs/heads/main")
	req, _ := http.NewRequest("POST", ps.URL+"/github.com/org/repo.git/git-receive-pack", bytes.NewReader(pktBody))
	req.SetBasicAuth("cid", "csecret")
	req.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	resp, err := client.Do(req)
	r.NoError(err)
	defer resp.Body.Close()

	a.Equal(http.StatusOK, resp.StatusCode)
	a.Equal("application/x-git-receive-pack-result", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	a.Contains(string(body), "ng refs/heads/main")
	a.Contains(string(body), "test-vm/*", "rejection should hint at allowed branch prefixes")
}

// ---------------------------------------------------------------------------
// E2E: push flow — authenticated but no push op → 403
// ---------------------------------------------------------------------------

func TestE2E_PushWithoutPushOp(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/git-receive-pack") {
			t.Fatal("should not reach upstream")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := &staticPolicyStore{
		policies: map[string]*Policy{
			"cid:csecret": {
				AllowedOps:   []Op{OpFetch}, // no OpPush
				AllowedRepos: []string{"org/repo"},
			},
		},
	}

	_, ps := testProxy(t, upstream, store)
	client := &http.Client{}

	pktBody := buildReceivePackBody("refs/heads/whatever")
	req, _ := http.NewRequest("POST", ps.URL+"/github.com/org/repo.git/git-receive-pack", bytes.NewReader(pktBody))
	req.SetBasicAuth("cid", "csecret")
	resp, err := client.Do(req)
	r.NoError(err)
	defer resp.Body.Close()
	a.Equal(http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	a.Contains(string(body), "ng refs/heads/whatever")
}

// ---------------------------------------------------------------------------
// E2E: push to disallowed repo → 403
// ---------------------------------------------------------------------------

func TestE2E_PushDisallowedRepo(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Fatal("should not reach upstream")
	}))
	defer upstream.Close()

	store := &staticPolicyStore{
		policies: map[string]*Policy{
			"cid:csecret": {
				AllowedOps:     []Op{OpFetch, OpPush},
				AllowedRepos:   []string{"org/repo"},
				BranchPrefixes: []string{"test-vm/*"},
			},
		},
	}

	_, ps := testProxy(t, upstream, store)
	client := &http.Client{}

	pktBody := buildReceivePackBody("refs/heads/test-vm/feat")
	req, _ := http.NewRequest("POST", ps.URL+"/github.com/evil-org/evil-repo.git/git-receive-pack", bytes.NewReader(pktBody))
	req.SetBasicAuth("cid", "csecret")
	resp, err := client.Do(req)
	r.NoError(err)
	defer resp.Body.Close()
	a.Equal(http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	a.Contains(string(body), "ng refs/heads/test-vm/feat")
}

// ---------------------------------------------------------------------------
// E2E: 401 challenge → retry with creds (simulates git credential helper)
// ---------------------------------------------------------------------------

func TestE2E_Push401ThenRetryWithCreds(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	var upstreamHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/x-git-receive-pack-advertisement")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := &staticPolicyStore{
		policies: map[string]*Policy{
			"cid:csecret": {
				AllowedOps:     []Op{OpFetch, OpPush},
				AllowedRepos:   []string{"org/repo"},
				BranchPrefixes: []string{"test-vm/*"},
			},
		},
	}

	_, ps := testProxy(t, upstream, store)
	client := &http.Client{}
	path := ps.URL + "/github.com/org/repo.git/info/refs?service=git-receive-pack"

	// Request 1: no auth → 401
	resp, err := http.Get(path)
	r.NoError(err)
	resp.Body.Close()
	a.Equal(http.StatusUnauthorized, resp.StatusCode)
	a.Contains(resp.Header.Get("WWW-Authenticate"), "Basic")
	a.Equal(0, upstreamHits)

	// Request 2: retry with creds → 200
	req, _ := http.NewRequest("GET", path, nil)
	req.SetBasicAuth("cid", "csecret")
	resp2, err := client.Do(req)
	r.NoError(err)
	resp2.Body.Close()
	a.Equal(http.StatusOK, resp2.StatusCode)
	a.Equal(1, upstreamHits)
}

// ---------------------------------------------------------------------------
// E2E: API requests (Bearer token, not Basic)
// ---------------------------------------------------------------------------

func TestE2E_APIBearerToken(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	var gotAuth string
	var gotHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		gotHost = req.Host
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	_, ps := testProxy(t, upstream, nil)

	resp, err := http.Get(ps.URL + "/github.com/repos/org/repo")
	r.NoError(err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	a.Equal(http.StatusOK, resp.StatusCode)
	a.Equal(`{"ok":true}`, string(body))
	a.Equal("Bearer test-gh-token", gotAuth)
	a.Equal("github.com", gotHost)
}

func TestE2E_GitUsesBasicAuth(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotAuth = req.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	_, ps := testProxy(t, upstream, nil)

	resp, err := http.Get(ps.URL + "/github.com/org/repo.git/info/refs?service=git-upload-pack")
	r.NoError(err)
	defer resp.Body.Close()

	a.Equal(http.StatusOK, resp.StatusCode)
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:test-gh-token"))
	a.Equal(expected, gotAuth)
}

// ---------------------------------------------------------------------------
// E2E: fetch disallowed repo → git ERR pkt-line with hint
// ---------------------------------------------------------------------------

func TestE2E_FetchDisallowedRepo(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		t.Fatal("should not reach upstream")
	}))
	defer upstream.Close()

	_, ps := testProxy(t, upstream, nil)

	resp, err := http.Get(ps.URL + "/github.com/evil-org/secret.git/info/refs?service=git-upload-pack")
	r.NoError(err)
	defer resp.Body.Close()

	a.Equal(http.StatusForbidden, resp.StatusCode)
	a.Equal("application/x-git-upload-pack-advertisement", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	a.Contains(string(body), "evil-org/secret")
	a.Contains(string(body), "GH_ALLOWED_REPOS")
}

// ---------------------------------------------------------------------------
// E2E: bad credentials fall back to default policy
// ---------------------------------------------------------------------------

func TestE2E_BadCredsFallback(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := &staticPolicyStore{policies: map[string]*Policy{}}
	_, ps := testProxy(t, upstream, store)
	client := &http.Client{}

	// Fetch with bad creds — falls back to default policy (which allows fetch)
	req, _ := http.NewRequest("GET", ps.URL+"/github.com/org/repo.git/info/refs?service=git-upload-pack", nil)
	req.SetBasicAuth("bad", "creds")
	resp, err := client.Do(req)
	r.NoError(err)
	defer resp.Body.Close()
	a.Equal(http.StatusOK, resp.StatusCode)

	// Push with bad creds — bad creds count as "authenticated" (creds != "") so no 401,
	// but default policy doesn't have OpPush → git protocol rejection
	pktBody := buildReceivePackBody("refs/heads/whatever")
	req2, _ := http.NewRequest("POST", ps.URL+"/github.com/org/repo.git/git-receive-pack", bytes.NewReader(pktBody))
	req2.SetBasicAuth("bad", "creds")
	resp2, err := client.Do(req2)
	r.NoError(err)
	defer resp2.Body.Close()
	a.Equal(http.StatusOK, resp2.StatusCode)
	body, _ := io.ReadAll(resp2.Body)
	a.Contains(string(body), "ng refs/heads/whatever")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func buildReceivePackBody(refName string) []byte {
	oldHash := "0000000000000000000000000000000000000000"
	newHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	line := oldHash + " " + newHash + " " + refName + "\x00report-status\n"
	pktLen := len(line) + 4
	var buf bytes.Buffer
	buf.WriteString(pktLenHexStr(pktLen))
	buf.WriteString(line)
	buf.WriteString("0000")
	return buf.Bytes()
}

func pktLenHexStr(n int) string {
	return fmt.Sprintf("%04x", n)
}

func farFuture() time.Time {
	return time.Now().Add(24 * time.Hour)
}

type rewriteTransport struct {
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Host = t.target
	req.URL.Scheme = "http"
	return http.DefaultTransport.RoundTrip(req)
}

type staticPolicyStore struct {
	policies map[string]*Policy
}

func (s *staticPolicyStore) Lookup(_ context.Context, proxyAuth string) (*Policy, error) {
	p, ok := s.policies[proxyAuth]
	if !ok {
		return nil, fmt.Errorf("unknown client")
	}
	return p, nil
}
