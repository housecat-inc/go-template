package srv

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	"srv.housecat.com/db/dbgen"
)

type OAuthConfig struct {
	ClientID      string
	ClientSecret  string
	Issuer        string
	SessionSecret string
}

func (s *Server) setUpOIDC(ctx context.Context) error {
	provider, err := oidc.NewProvider(ctx, s.OAuth.Issuer)
	if err != nil {
		return errors.Wrap(err, "oidc discovery")
	}
	s.oidcProvider = provider
	s.oauth2Config = &oauth2.Config{
		ClientID:     s.OAuth.ClientID,
		ClientSecret: s.OAuth.ClientSecret,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
	return nil
}

func (s *Server) getSession(r *http.Request) (dbgen.Session, error) {
	if s.DB == nil {
		return dbgen.Session{}, errors.New("no database")
	}
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return dbgen.Session{}, errors.Wrap(err, "no session cookie")
	}
	q := dbgen.New(s.DB)
	return q.GetSession(r.Context(), cookie.Value)
}

func (s *Server) RequireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()
		var logoutURL, userEmail, userID string

		session, err := s.getSession(r)
		if err == nil {
			logoutURL = "/auth/logout"
			userEmail = session.Email
			userID = session.UserID
		} else {
			userID = strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
			userEmail = strings.TrimSpace(r.Header.Get("X-ExeDev-Email"))
			logoutURL = "/auth/logout"
		}

		if userID == "" {
			return c.Redirect(http.StatusFound, "/")
		}

		// When authenticated via exe.dev headers but no session cookie,
		// create one so cookie-only clients (e.g. browser tool) work.
		if err != nil && userID != "" && s.DB != nil {
			s.ensureSessionCookie(c, userID, userEmail)
		}

		c.Set("logoutURL", logoutURL)
		c.Set("userEmail", userEmail)
		c.Set("userID", userID)
		return next(c)
	}
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

	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing state cookie")
	}
	if c.QueryParam("state") != stateCookie.Value {
		return echo.NewHTTPError(http.StatusBadRequest, "state mismatch")
	}
	c.SetCookie(&http.Cookie{
		Name:   "oauth_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	cfg := *s.oauth2Config
	cfg.RedirectURL = s.callbackURL(r)
	token, err := cfg.Exchange(ctx, c.QueryParam("code"))
	if err != nil {
		slog.Error("oauth exchange", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, "failed to exchange code")
	}

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

	return c.Redirect(http.StatusFound, "/home")
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

	if r.Header.Get("X-ExeDev-UserID") != "" {
		return c.HTML(http.StatusOK, `<!DOCTYPE html>
<html><body>
<form id="f" method="POST" action="/__exe.dev/logout"></form>
<script>document.getElementById('f').submit()</script>
</body></html>`)
	}

	return c.Redirect(http.StatusFound, "/")
}

func (s *Server) ensureSessionCookie(c echo.Context, userID, email string) {
	r := c.Request()
	sessionID, err := randomHex(32)
	if err != nil {
		slog.Warn("generate session id", "error", err)
		return
	}

	q := dbgen.New(s.DB)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := q.InsertSession(r.Context(), dbgen.InsertSessionParams{
		ID:        sessionID,
		UserID:    userID,
		Email:     email,
		ExpiresAt: expiresAt,
	}); err != nil {
		slog.Warn("insert session for exe.dev user", "error", err)
		return
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
}

func (s *Server) callbackURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/auth/callback"
}

func loginURLForRequest(r *http.Request) string {
	path := r.URL.RequestURI()
	v := url.Values{}
	v.Set("redirect", path)
	return "/__exe.dev/login?" + v.Encode()
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
