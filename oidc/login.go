package oidc

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// SessionResolver looks up the current user session.
// Returns userID, email, error.
type SessionResolver func(r *http.Request) (string, string, error)

// LoginHandler returns an Echo handler for /oidc/login.
// The OP redirects here during the authorize flow. If the user has a session,
// we complete the auth request and redirect back to the OP callback.
func LoginHandler(storage *Storage, provider op.OpenIDProvider, resolveSession SessionResolver) echo.HandlerFunc {
	return func(c echo.Context) error {
		r := c.Request()
		authRequestID := c.QueryParam("authRequestID")
		if authRequestID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "authRequestID required")
		}

		userID, email, err := resolveSession(r)
		if err != nil {
			if isLoopbackRequest(r) {
				userID = "browser-tool"
				email = "tool@localhost"
			} else {
				returnURL := r.URL.String()
				loginURL := "/?redirect=" + url.QueryEscape(returnURL)
				return c.Redirect(http.StatusFound, loginURL)
			}
		}

		if err := storage.CompleteAuthRequest(r.Context(), authRequestID, userID, email); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid auth request")
		}

		callbackURL := op.AuthCallbackURL(provider)(r.Context(), authRequestID)
		return c.Redirect(http.StatusFound, callbackURL)
	}
}

func isLoopbackRequest(r *http.Request) bool {
	if !strings.HasPrefix(r.Host, "localhost") {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
