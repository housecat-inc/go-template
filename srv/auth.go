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
	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/housecat-inc/go-template/db/dbgen"
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
	provider, err := rp.NewRelyingPartyOIDC(ctx, s.OAuth.Issuer, s.OAuth.ClientID, s.OAuth.ClientSecret, "", []string{oidc.ScopeOpenID, oidc.ScopeEmail, oidc.ScopeProfile})
	if err != nil {
		return errors.Wrap(err, "oidc discovery")
	}
	s.relyingParty = provider
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

		// Trust localhost unconditionally — this is the agentic browser tool.
		// Real users come through the proxy with a different Host header.
		if strings.HasPrefix(r.Host, "localhost") {
			c.Set("logoutURL", "")
			c.Set("userEmail", "tool@localhost")
			c.Set("userID", "browser-tool")
			return next(c)
		}

		var logoutURL, userEmail, userID string

		session, err := s.getSession(r)
		if err == nil {
			logoutURL = "/auth/logout"
			userEmail = session.Email
			userID = session.UserID
		} else {
			userID = strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
			userEmail = strings.TrimSpace(r.Header.Get("X-ExeDev-Email"))
			logoutURL = "/__exe.dev/logout?redirect=/"
		}

		if userID == "" {
			return c.Redirect(http.StatusFound, "/")
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
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	cfg := *s.relyingParty.OAuthConfig()
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
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	cfg := *s.relyingParty.OAuthConfig()
	cfg.RedirectURL = s.callbackURL(r)
	oauthToken, err := cfg.Exchange(ctx, c.QueryParam("code"))
	if err != nil {
		slog.Error("oauth exchange", "error", err, "redirect_uri", cfg.RedirectURL, "token_url", cfg.Endpoint.TokenURL)
		return echo.NewHTTPError(http.StatusBadRequest, "failed to exchange code")
	}

	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "no id_token in response")
	}
	claims, err := rp.VerifyTokens[*oidc.IDTokenClaims](ctx, oauthToken.AccessToken, rawIDToken, s.relyingParty.IDTokenVerifier())
	if err != nil {
		slog.Error("id token verify", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id_token")
	}

	userEmail := claims.Email
	userSubject := claims.Subject

	// If the ID token didn't include email, fetch from userinfo endpoint.
	if userEmail == "" {
		info, err := rp.Userinfo[*oidc.UserInfo](ctx, oauthToken.AccessToken, oauthToken.TokenType, claims.Subject, s.relyingParty)
		if err != nil {
			slog.Warn("userinfo fetch", "error", err)
		} else {
			userEmail = info.Email
			if userEmail == "" {
				userEmail = info.PreferredUsername
			}
			if userSubject == "" {
				userSubject = info.Subject
			}
		}
	}

	sessionID, err := randomHex(32)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate session")
	}

	q := dbgen.New(s.DB)
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := q.InsertSession(ctx, dbgen.InsertSessionParams{
		ID:        sessionID,
		UserID:    userSubject,
		Email:     userEmail,
		ExpiresAt: expiresAt,
	}); err != nil {
		slog.Error("insert session", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create session")
	}

	c.SetCookie(&http.Cookie{
		Name:     "session_id",
		Value:    s.signSessionID(sessionID),
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60,
	})

	return c.Redirect(http.StatusFound, "/home")
}

func (s *Server) HandleAuthLogout(c echo.Context) error {
	r := c.Request()

	if cookie, err := r.Cookie("session_id"); err == nil && s.DB != nil {
		if sessionID, verr := s.verifySessionCookie(cookie.Value); verr == nil {
			q := dbgen.New(s.DB)
			_ = q.DeleteSession(r.Context(), sessionID)
		}
	}

	c.SetCookie(&http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	return c.Redirect(http.StatusFound, "/")
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") != "" {
		return strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	// Behind a proxy that doesn't set X-Forwarded-Proto — assume HTTPS
	// unless the request is clearly local development.
	return !strings.HasPrefix(r.Host, "localhost")
}

func (s *Server) callbackURL(r *http.Request) string {
	scheme := "https"
	if !isSecureRequest(r) {
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

func (s *Server) HandleAuthExeDev(c echo.Context) error {
	r := c.Request()
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	userEmail := strings.TrimSpace(r.Header.Get("X-ExeDev-Email"))
	if userID == "" || userEmail == "" {
		return c.Redirect(http.StatusFound, loginURLForRequest(r))
	}
	return s.createSessionAndRedirect(c, userID, userEmail)
}

func (s *Server) createSessionAndRedirect(c echo.Context, userID, email string) error {
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
		ExpiresAt: expiresAt,
	}); err != nil {
		return errors.Wrap(err, "insert session")
	}

	c.SetCookie(&http.Cookie{
		Name:     "session_id",
		Value:    s.signSessionID(sessionID),
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
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
