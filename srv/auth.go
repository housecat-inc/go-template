package srv

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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

	"github.com/housecat-inc/auth/db/dbgen"
)

type OAuthConfig struct {
	ClientID      string
	ClientSecret  string
	Issuer        string
	SessionSecret string
}

func (s *Server) signSessionID(id string) string {
	if s.sessionSecret == "" {
		return id
	}
	mac := hmac.New(sha256.New, []byte(s.sessionSecret))
	mac.Write([]byte(id))
	sig := hex.EncodeToString(mac.Sum(nil))
	return id + "." + sig
}

func (s *Server) verifySessionCookie(value string) (string, error) {
	if s.sessionSecret == "" {
		return value, nil
	}
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return "", errors.New("invalid session cookie format")
	}
	id, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(s.sessionSecret))
	mac.Write([]byte(id))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", errors.New("invalid session cookie signature")
	}
	return id, nil
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
	sessionID, err := s.verifySessionCookie(cookie.Value)
	if err != nil {
		return dbgen.Session{}, errors.Wrap(err, "verify session cookie")
	}
	q := dbgen.New(s.DB)
	return q.GetSession(r.Context(), sessionID)
}

func (s *Server) RequireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()

		var logoutURL, provider, userEmail, userID string

		session, err := s.getSession(r)
		if err == nil {
			logoutURL = "/auth/logout"
			provider = session.Provider
			userEmail = session.Email
			userID = session.UserID
		} else if strings.HasPrefix(r.Host, "localhost") {
			logoutURL = ""
			provider = "localhost"
			userEmail = "tool@localhost"
			userID = "browser-tool"
		} else {
			userID = strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
			userEmail = strings.TrimSpace(r.Header.Get("X-ExeDev-Email"))
			logoutURL = "/__exe.dev/logout?redirect=/"
			provider = "exe.dev"
		}

		if userID == "" {
			return c.Redirect(http.StatusFound, "/")
		}

		c.Set("logoutURL", logoutURL)
		c.Set("provider", provider)
		c.Set("userEmail", userEmail)
		c.Set("userID", userID)
		return next(c)
	}
}

func isAdmin(email string) bool {
	return strings.HasSuffix(strings.ToLower(email), "@housecat.com")
}

func (s *Server) RequireAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		email, _ := c.Get("userEmail").(string)
		if !isAdmin(email) {
			return echo.NewHTTPError(http.StatusForbidden, "admin access required")
		}
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
		Name:     "oauth_state",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
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

	return s.createSessionAndRedirect(c, claims.Sub, claims.Email, "Google")
}

func (s *Server) HandleAuthLogout(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()

	if cookie, err := r.Cookie("session_id"); err == nil && s.DB != nil {
		if sessionID, verr := s.verifySessionCookie(cookie.Value); verr == nil {
			q := dbgen.New(s.DB)
			if sess, err := q.GetSession(ctx, sessionID); err == nil {
				provider := sess.Provider
				_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
					ActorID:    sess.UserID,
					ActorType:  "user",
					Action:     "logged_out",
					ObjectID:   sess.UserID,
					ObjectType: "user",
					Metadata:   &provider,
				})
			}
			_ = q.DeleteSession(ctx, sessionID)
		}
	}

	c.SetCookie(&http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	return c.Redirect(http.StatusFound, "/")
}

func (s *Server) callbackURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/auth/google/callback"
}

func loginURLForRequest(r *http.Request) string {
	path := r.URL.RequestURI()
	v := url.Values{}
	v.Set("redirect", path)
	return "/__exe.dev/login?" + v.Encode()
}

func (s *Server) HandleAuthExeDev(c echo.Context) error {
	r := c.Request()
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	userEmail := strings.TrimSpace(r.Header.Get("X-ExeDev-Email"))
	if userID == "" || userEmail == "" {
		return c.Redirect(http.StatusFound, loginURLForRequest(r))
	}
	return s.createSessionAndRedirect(c, userID, userEmail, "exe.dev")
}

func (s *Server) createSessionAndRedirect(c echo.Context, userID, email, provider string) error {
	r := c.Request()
	ctx := r.Context()

	sessionID, err := randomHex(32)
	if err != nil {
		return errors.Wrap(err, "generate session id")
	}

	q := dbgen.New(s.DB)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := q.InsertSession(ctx, dbgen.InsertSessionParams{
		ID:        sessionID,
		UserID:    userID,
		Email:     email,
		Provider:  provider,
		ExpiresAt: expiresAt,
	}); err != nil {
		return errors.Wrap(err, "insert session")
	}

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "logged_in",
		ObjectID:   userID,
		ObjectType: "user",
		Metadata:   &provider,
	})

	c.SetCookie(&http.Cookie{
		Name:     "session_id",
		Value:    s.signSessionID(sessionID),
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})

	return c.Redirect(http.StatusFound, "/home")
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
