package srv

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	"srv.housecat.com/db"
	"srv.housecat.com/db/dbgen"
	"srv.housecat.com/ui/blocks/auth"
	"srv.housecat.com/ui/pages"
)

type OAuthConfig struct {
	ClientID      string
	ClientSecret  string
	SessionSecret string
	Issuer        string // e.g. "https://auth.housecat.com"
}

type Server struct {
	DB           *sql.DB
	Hostname     string
	AssetsDir    string
	OAuth        OAuthConfig
	oidcProvider *oidc.Provider
	oauth2Config *oauth2.Config
}

func New(dbPath, hostname string, oauthCfg OAuthConfig) (*Server, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	srv := &Server{
		Hostname:  hostname,
		AssetsDir: filepath.Join(baseDir, "..", "assets"),
		OAuth:     oauthCfg,
	}
	if err := srv.setUpDatabase(dbPath); err != nil {
		return nil, err
	}
	if oauthCfg.ClientID != "" {
		if err := srv.setUpOIDC(context.Background()); err != nil {
			return nil, fmt.Errorf("setup oidc: %w", err)
		}
	}
	return srv, nil
}

func (s *Server) setUpOIDC(ctx context.Context) error {
	provider, err := oidc.NewProvider(ctx, s.OAuth.Issuer)
	if err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}
	s.oidcProvider = provider
	s.oauth2Config = &oauth2.Config{
		ClientID:     s.OAuth.ClientID,
		ClientSecret: s.OAuth.ClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		// RedirectURL is set per-request based on the Host header
	}
	return nil
}

func (s *Server) HandleRoot(c echo.Context) error {
	r := c.Request()
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	userEmail := strings.TrimSpace(r.Header.Get("X-ExeDev-Email"))
	logoutURL := "/__exe.dev/logout?redirect=/"

	// Check session cookie if no exe.dev headers
	if userID == "" && s.DB != nil {
		if cookie, err := r.Cookie("session_id"); err == nil {
			q := dbgen.New(s.DB)
			session, err := q.GetSession(r.Context(), cookie.Value)
			if err == nil {
				userID = session.UserID
				userEmail = session.Email
				logoutURL = "/auth/logout"
			}
		}
	}

	if userID == "" {
		googleURL := ""
		if s.oauth2Config != nil {
			googleURL = "/auth/google"
		}
		component := auth.SignInPage(loginURLForRequest(r), googleURL)
		return component.Render(r.Context(), c.Response())
	}

	now := time.Now()

	var count int64
	if s.DB != nil {
		q := dbgen.New(s.DB)
		if r.Method == "GET" {
			err := q.InsertActivity(r.Context(), dbgen.InsertActivityParams{
				ActorID:   userID,
				ActorType: "user",
				Action:    "logged_in",
			})
			if err != nil {
				slog.Warn("insert activity", "error", err, "user_id", userID)
			}
		}
		activities, err := q.ListActivitiesByActor(r.Context(), dbgen.ListActivitiesByActorParams{
			ActorID: userID,
			Limit:   1000,
		})
		if err == nil {
			count = int64(len(activities))
		}
	}

	data := pages.PageData{
		Hostname:      s.Hostname,
		Now:           now.Format(time.RFC3339),
		UserEmail:     userEmail,
		ActivityCount: count,
		LoginURL:      loginURLForRequest(r),
		LogoutURL:     logoutURL,
		Headers:       buildHeaderEntries(r),
	}

	component := pages.Welcome(data)
	return component.Render(r.Context(), c.Response())
}

func loginURLForRequest(r *http.Request) string {
	path := r.URL.RequestURI()
	v := url.Values{}
	v.Set("redirect", path)
	return "/__exe.dev/login?" + v.Encode()
}

func mainDomainFromHost(h string) string {
	host, port, err := net.SplitHostPort(h)
	if err != nil {
		host = strings.TrimSpace(h)
	}
	if port != "" {
		port = ":" + port
	}
	if strings.HasSuffix(host, ".exe.cloud") || host == "exe.cloud" {
		return "exe.cloud" + port
	}
	if strings.HasSuffix(host, ".exe.dev") || host == "exe.dev" {
		return "exe.dev"
	}
	return host
}

func (s *Server) setUpDatabase(dbPath string) error {
	wdb, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	s.DB = wdb
	if err := db.RunMigrations(wdb); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

func (s *Server) Serve(addr string) error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.GET("/", s.HandleRoot)
	if s.oauth2Config != nil {
		e.GET("/auth/google", s.HandleAuthGoogle)
		e.GET("/auth/callback", s.HandleAuthCallback)
	}
	e.POST("/auth/logout", s.HandleAuthLogout)
	e.Static("/assets", s.AssetsDir)

	slog.Info("starting server", "addr", addr)
	return e.Start(addr)
}

func (s *Server) callbackURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/auth/callback"
}

func (s *Server) HandleAuthGoogle(c echo.Context) error {
	r := c.Request()

	state, err := randomHex(16)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate state")
	}

	c.SetCookie(&http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	cfg := *s.oauth2Config
	cfg.RedirectURL = s.callbackURL(r)
	return c.Redirect(http.StatusFound, cfg.AuthCodeURL(state))
}

func (s *Server) HandleAuthCallback(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()

	// Verify state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing state cookie")
	}
	if c.QueryParam("state") != stateCookie.Value {
		return echo.NewHTTPError(http.StatusBadRequest, "state mismatch")
	}
	// Clear state cookie
	c.SetCookie(&http.Cookie{
		Name:   "oauth_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	// Exchange code for token
	cfg := *s.oauth2Config
	cfg.RedirectURL = s.callbackURL(r)
	token, err := cfg.Exchange(ctx, c.QueryParam("code"))
	if err != nil {
		slog.Error("oauth exchange", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, "failed to exchange code")
	}

	// Extract and verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "no id_token in response")
	}
	verifier := s.oidcProvider.Verifier(&oidc.Config{ClientID: s.OAuth.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		slog.Error("id token verify", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id_token")
	}

	var claims struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to parse claims")
	}

	// Create session
	sessionID, err := randomHex(32)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate session")
	}

	q := dbgen.New(s.DB)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := q.InsertSession(ctx, dbgen.InsertSessionParams{
		ID:        sessionID,
		UserID:    claims.Sub,
		Email:     claims.Email,
		ExpiresAt: expiresAt,
	}); err != nil {
		slog.Error("insert session", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create session")
	}

	c.SetCookie(&http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})

	return c.Redirect(http.StatusFound, "/")
}

func (s *Server) HandleAuthLogout(c echo.Context) error {
	r := c.Request()

	if cookie, err := r.Cookie("session_id"); err == nil && s.DB != nil {
		q := dbgen.New(s.DB)
		_ = q.DeleteSession(r.Context(), cookie.Value)
	}

	c.SetCookie(&http.Cookie{
		Name:   "session_id",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	return c.Redirect(http.StatusFound, "/")
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func buildHeaderEntries(r *http.Request) []pages.HeaderEntry {
	if r == nil {
		return nil
	}

	headers := make([]pages.HeaderEntry, 0, len(r.Header)+1)
	for name, values := range r.Header {
		lower := strings.ToLower(name)
		headers = append(headers, pages.HeaderEntry{
			Name:       name,
			Values:     values,
			AddedByExe: strings.HasPrefix(lower, "x-exedev-") || strings.HasPrefix(lower, "x-forwarded-"),
		})
	}
	if r.Host != "" {
		headers = append(headers, pages.HeaderEntry{
			Name:   "Host",
			Values: []string{r.Host},
		})
	}

	sort.Slice(headers, func(i, j int) bool {
		return strings.ToLower(headers[i].Name) < strings.ToLower(headers[j].Name)
	})
	return headers
}
