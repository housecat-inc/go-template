package oidc

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/housecat-inc/auth/ui/blocks/auth"
)

func emailDomain(email string) string {
	_, domain, _ := strings.Cut(email, "@")
	return strings.ToLower(domain)
}

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

		authReq, err := storage.AuthRequestByID(r.Context(), authRequestID)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid auth request")
		}

		loginHint := ""
		if ar, ok := authReq.(*AuthRequest); ok {
			loginHint = ar.GetLoginHint()
		}

		userID, email, err := resolveSession(r)
		if err != nil {
			if isLoopbackRequest(r) {
				userID = "browser-tool"
				email = "tool@localhost"
			} else if loginHint == "app" {
				returnURL := r.URL.String()
				googleURL := "/auth/google?redirect=" + url.QueryEscape(returnURL)
				return auth.AppLoginPage(googleURL).Render(r.Context(), c.Response())
			} else {
				returnURL := r.URL.String()
				loginURL := "/?redirect=" + url.QueryEscape(returnURL)
				return c.Redirect(http.StatusFound, loginURL)
			}
		}

		if !isLoopbackRequest(r) && loginHint != "app" {
			client, err := storage.q().GetOidcClientByClientID(r.Context(), authReq.GetClientID())
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "unknown client")
			}

			email = strings.ToLower(strings.TrimSpace(email))
			domain := emailDomain(email)
			rules, err := storage.q().ListClientAccessByClientID(r.Context(), client.ID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "access check failed")
			}
			matched := false
			if len(rules) == 0 {
				matched = domain == "housecat.com"
			} else {
				for _, rule := range rules {
					if rule.Email == email || rule.Domain == domain {
						matched = true
						break
					}
				}
			}
			if !matched {
				return echo.NewHTTPError(http.StatusForbidden, "you don't have access to this application")
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
