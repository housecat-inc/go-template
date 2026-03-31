package integration

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/housecat-inc/auth/db/dbgen"
	hcmcp "github.com/housecat-inc/auth/mcp"
	"github.com/modelcontextprotocol/go-sdk/auth"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	_ "modernc.org/sqlite"
)

const (
	imogenSubject = "112892112702904839570"
	prodDB        = "/opt/srv/data/db.sqlite3"
	envFile       = "/opt/srv/data/.env"
)

type toolTest struct {
	name    string
	tool    string
	args    string
	levels  []string
	wantErr string
}

func parseArgs(t *testing.T, s string) map[string]any {
	t.Helper()
	if s == "" || s == "{}" {
		return map[string]any{}
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(s), &args); err != nil {
		t.Fatalf("bad test args JSON: %v", err)
	}
	return args
}

func lookupAtLevel(service, realToken string, levels ...string) hcmcp.TokenLookup {
	connected := map[string]bool{}
	for _, l := range levels {
		connected[l] = true
	}
	return func(ctx context.Context, subject, svc, level string) (string, error) {
		if svc == service && connected[level] {
			return realToken, nil
		}
		return "", hcmcp.ErrTokenNotFound
	}
}

func runToolTests(t *testing.T, service string, token string, tests []toolTest) {
	ctx := context.Background()
	upstreamTools := hcmcp.DefaultUpstreamTools()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			r := require.New(t)

			lookup := lookupAtLevel(service, token, tt.levels...)
			server := hcmcp.NewServer("https://test.example.com", lookup, nil, nil, upstreamTools)

			streamHandler := gomcp.NewStreamableHTTPHandler(
				func(req *http.Request) *gomcp.Server { return server },
				nil,
			)
			verifier := func(ctx context.Context, tok string, req *http.Request) (*auth.TokenInfo, error) {
				return &auth.TokenInfo{
					UserID:     imogenSubject,
					Expiration: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC),
				}, nil
			}
			handler := auth.RequireBearerToken(verifier, nil)(streamHandler)
			httpServer := httptest.NewServer(handler)
			defer httpServer.Close()

			transport := &gomcp.StreamableClientTransport{
				Endpoint: httpServer.URL,
				HTTPClient: &http.Client{
					Transport: &bearerTransport{token: "test-token"},
				},
			}
			client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
			session, err := client.Connect(ctx, transport, nil)
			r.NoError(err)
			defer session.Close()

			args := parseArgs(t, tt.args)
			res, err := session.CallTool(ctx, &gomcp.CallToolParams{
				Name:      tt.tool,
				Arguments: args,
			})
			r.NoError(err)

			text := res.Content[0].(*gomcp.TextContent).Text

			if tt.wantErr != "" {
				a.True(res.IsError, "expected error for %s: got %s", tt.name, text)
				a.Contains(text, tt.wantErr)
			} else if res.IsError {
				a.NotContains(text, "level not connected", "unexpected gating error: %s", text)
				a.NotContains(text, "only allows creating private pages", "unexpected gating error: %s", text)
			}
		})
	}
}

type bearerTransport struct {
	token string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(req)
}

// --- DB and token helpers ---

func openDB(t *testing.T) (*sql.DB, *dbgen.Queries) {
	t.Helper()
	if _, err := os.Stat(prodDB); err != nil {
		t.Skipf("prod db not available: %v", err)
	}
	db, err := sql.Open("sqlite", prodDB)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dbgen.New(db)
}

func tokenForSubject(t *testing.T, db *sql.DB, q *dbgen.Queries, subject, service, level string) string {
	t.Helper()
	ctx := context.Background()
	tok, err := q.GetOAuthToken(ctx, dbgen.GetOAuthTokenParams{
		Subject: subject,
		Service: service,
		Level:   level,
	})
	if err != nil {
		t.Fatalf("no %s/%s token for subject %s — connect at /connect/%s/enable/%s", service, level, subject, service, level)
	}
	if tok.ExpiresAt != nil && tok.ExpiresAt.Before(time.Now().Add(30*time.Second)) && tok.RefreshToken != "" {
		if refreshed := refreshToken(t, db, q, tok); refreshed != "" {
			return refreshed
		}
	}
	return tok.AccessToken
}

func loadEnv(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(envFile)
	if err != nil {
		t.Skipf("env file not available: %v", err)
	}
	defer f.Close()
	env := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			env[k] = v
		}
	}
	return env
}

func refreshToken(t *testing.T, db *sql.DB, q *dbgen.Queries, tok dbgen.OauthToken) string {
	t.Helper()
	env := loadEnv(t)

	var endpoint oauth2.Endpoint
	var clientID, clientSecret string

	switch tok.Provider {
	case "google":
		endpoint = oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		}
		clientID = env["GOOGLE_CLIENT_ID"]
		clientSecret = env["GOOGLE_CLIENT_SECRET"]
	case "notion":
		endpoint = oauth2.Endpoint{
			TokenURL:  "https://mcp.notion.com/token",
			AuthStyle: oauth2.AuthStyleInParams,
		}
		clientID = tok.ClientID
	default:
		t.Skipf("refresh not supported for provider %s", tok.Provider)
		return ""
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     endpoint,
	}

	oldToken := &oauth2.Token{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
	}
	if tok.ExpiresAt != nil {
		oldToken.Expiry = *tok.ExpiresAt
	}

	newToken, err := cfg.TokenSource(context.Background(), oldToken).Token()
	if err != nil {
		t.Logf("token refresh failed: %v", err)
		return ""
	}

	var expiresAt *time.Time
	if !newToken.Expiry.IsZero() {
		expiresAt = &newToken.Expiry
	}
	refreshTok := newToken.RefreshToken
	if refreshTok == "" {
		refreshTok = tok.RefreshToken
	}

	if err := q.UpsertOAuthToken(context.Background(), dbgen.UpsertOAuthTokenParams{
		AccessToken:  newToken.AccessToken,
		ClientID:     tok.ClientID,
		ExpiresAt:    expiresAt,
		Level:        tok.Level,
		Provider:     tok.Provider,
		RefreshToken: refreshTok,
		Scopes:       tok.Scopes,
		Service:      tok.Service,
		Subject:      tok.Subject,
	}); err != nil {
		slog.Warn("failed to persist refreshed token", "error", err)
	}

	return newToken.AccessToken
}
