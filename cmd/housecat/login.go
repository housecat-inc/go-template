package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// cachedToken is the on-disk format for a cached OAuth token.
type cachedToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
	TokenURL     string    `json:"token_url,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
}

// tokenCachePath returns the path to the cached token file for a given server URL.
func tokenCachePath(serverURL string) string {
	h := sha256.Sum256([]byte(serverURL))
	name := hex.EncodeToString(h[:8])
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "housecat", "tokens", name+".json")
}

// loadCachedToken loads a cached token if it exists. If the access token is
// expired but a refresh token is available, it attempts a silent refresh.
func loadCachedToken(ctx context.Context, serverURL string) (string, bool) {
	data, err := os.ReadFile(tokenCachePath(serverURL))
	if err != nil {
		return "", false
	}
	var ct cachedToken
	if err := json.Unmarshal(data, &ct); err != nil {
		return "", false
	}
	// If access token is still valid, use it
	if time.Until(ct.Expiry) >= time.Minute {
		return ct.AccessToken, true
	}
	// Try refresh if we have a refresh token
	if ct.RefreshToken == "" || ct.TokenURL == "" || ct.ClientID == "" {
		return "", false
	}
	slog.Info("access token expired, refreshing")
	cfg := &oauth2.Config{
		ClientID:     ct.ClientID,
		ClientSecret: ct.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL:  ct.TokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	oldToken := &oauth2.Token{
		RefreshToken: ct.RefreshToken,
	}
	newToken, err := cfg.TokenSource(ctx, oldToken).Token()
	if err != nil {
		slog.Warn("token refresh failed", "err", err)
		return "", false
	}
	saveCachedToken(serverURL, newToken, ct.TokenURL, ct.ClientID, ct.ClientSecret)
	slog.Info("token refreshed")
	return newToken.AccessToken, true
}

// saveCachedToken saves a token to the cache.
func saveCachedToken(serverURL string, token *oauth2.Token, tokenURL, clientID, clientSecret string) {
	path := tokenCachePath(serverURL)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		slog.Warn("could not create token cache dir", "err", err)
		return
	}
	ct := cachedToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		TokenURL:     tokenURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}
	data, err := json.Marshal(ct)
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		slog.Warn("could not cache token", "err", err)
	}
}

// login performs the OAuth authorization code flow to obtain an access token.
// It discovers the OIDC configuration, dynamically registers a client,
// opens the browser for authorization, and exchanges the code for a token.
func login(ctx context.Context, serverURL string) (string, error) {
	// Derive the issuer URL from the server URL (strip path)
	issuer, err := issuerFromServerURL(serverURL)
	if err != nil {
		return "", err
	}

	// Discover OIDC configuration
	oidcConfig, err := discoverOIDC(ctx, issuer)
	if err != nil {
		return "", fmt.Errorf("OIDC discovery: %w", err)
	}

	// Start local HTTP server for redirect
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)

	// Register client dynamically
	clientID, clientSecret, err := registerClient(ctx, oidcConfig.RegistrationEndpoint, redirectURL)
	if err != nil {
		return "", fmt.Errorf("client registration: %w", err)
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   oidcConfig.AuthorizationEndpoint,
			TokenURL:  oidcConfig.TokenEndpoint,
			AuthStyle: oauth2.AuthStyleInParams,
		},
		RedirectURL: redirectURL,
		Scopes:      []string{"openid", "email", "profile", "offline_access"},
	}

	// Generate PKCE verifier and state
	codeVerifier := oauth2.GenerateVerifier()
	state := randomHex(16)

	authURL := cfg.AuthCodeURL(state,
		oauth2.S256ChallengeOption(codeVerifier),
	)

	// Open browser
	slog.Info("opening browser for login")
	if err := exec.Command("open", authURL).Start(); err != nil {
		slog.Warn("could not open browser, open manually", "url", authURL)
	}

	// Wait for the callback
	codeCh := make(chan callbackResult, 1)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			if errMsg := q.Get("error"); errMsg != "" {
				codeCh <- callbackResult{err: fmt.Errorf("authorization error: %s: %s", errMsg, q.Get("error_description"))}
			} else {
				codeCh <- callbackResult{code: q.Get("code"), state: q.Get("state")}
			}
			fmt.Fprintln(w, "Login successful! You can close this tab.")
		}),
	}

	go srv.Serve(listener)
	defer srv.Shutdown(ctx)

	var result callbackResult
	select {
	case result = <-codeCh:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if result.err != nil {
		return "", result.err
	}
	if result.state != state {
		return "", fmt.Errorf("state mismatch")
	}

	// Exchange code for token
	token, err := cfg.Exchange(ctx, result.code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}

	saveCachedToken(serverURL, token, oidcConfig.TokenEndpoint, clientID, clientSecret)
	slog.Info("login successful")
	return token.AccessToken, nil
}

type callbackResult struct {
	code  string
	state string
	err   error
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

func issuerFromServerURL(serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}
	u.Path = ""
	u.RawQuery = ""
	return u.String(), nil
}

func discoverOIDC(ctx context.Context, issuer string) (*oidcDiscovery, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("discovery returned %d", resp.StatusCode)
	}
	var config oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

func registerClient(ctx context.Context, endpoint, redirectURL string) (clientID, clientSecret string, err error) {
	body := fmt.Sprintf(`{"client_name":"housecat","redirect_uris":[%q]}`, redirectURL)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		return "", "", fmt.Errorf("registration returned %d", resp.StatusCode)
	}

	var result struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}
	return result.ClientID, result.ClientSecret, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
