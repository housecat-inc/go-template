package srv

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/mcp"
)

// oauthClientRegistration is the response from dynamic OAuth client registration
// (RFC 7591) used by upstream MCP services (Attio, Granola, Notion).
type oauthClientRegistration struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

var googleServices = map[string]bool{
	"gcal":    true,
	"gdocs":   true,
	"gdrive":  true,
	"gmail":   true,
	"gsheets": true,
}

func (s *Server) oauthConfigForService(r *http.Request, service, level string) (*oauth2.Config, error) {
	svc, ok := servicesByID[service]
	if !ok {
		return nil, errors.Newf("unknown service: %s", service)
	}

	var lvl *mcp.Connection
	for i := range svc.Connections {
		if svc.Connections[i].Level == level {
			lvl = &svc.Connections[i]
			break
		}
	}
	if lvl == nil {
		return nil, errors.Newf("unknown level %s for service %s", level, service)
	}

	callbackURL := s.serviceCallbackURL(r, service)

	if googleServices[service] {
		if s.OAuth.ClientID == "" {
			return nil, errors.New("google oauth not configured")
		}
		return &oauth2.Config{
			ClientID:     s.OAuth.ClientID,
			ClientSecret: s.OAuth.ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL: "https://oauth2.googleapis.com/token",
			},
			RedirectURL: callbackURL,
			Scopes:      lvl.Scopes,
		}, nil
	}

	switch service {
	case "slack":
		if s.SlackOAuth.ClientID == "" {
			return nil, errors.New("slack oauth not configured")
		}
		return &oauth2.Config{
			ClientID:     s.SlackOAuth.ClientID,
			ClientSecret: s.SlackOAuth.ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:   "https://slack.com/oauth/v2/authorize",
				TokenURL:  "https://slack.com/api/oauth.v2.access",
				AuthStyle: oauth2.AuthStyleInParams,
			},
			RedirectURL: callbackURL,
			Scopes:      lvl.Scopes,
		}, nil
	default:
		return nil, errors.Newf("oauth not supported for service: %s", service)
	}
}

func (s *Server) serviceCallbackURL(r *http.Request, service string) string {
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	provider := service
	if googleServices[service] {
		provider = "google"
	}
	return scheme + "://" + r.Host + "/connect/" + provider + "/callback"
}

func (s *Server) HandleConnectEnable(c echo.Context) error {
	service := c.Param("service")
	if service == "attio" {
		return s.HandleAttioConnectEnable(c)
	}
	if service == "granola" {
		return s.HandleGranolaConnectEnable(c)
	}
	if service == "notion" {
		return s.HandleNotionConnectEnable(c)
	}

	r := c.Request()
	level := c.Param("level")
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	cfg, err := s.oauthConfigForService(r, service, level)
	if err != nil {
		slog.Error("oauth config", "service", service, "level", level, "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	state, err := randomHex(16)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate state")
	}

	stateValue := service + ":" + level + ":" + state
	c.SetCookie(&http.Cookie{
		Name:     "connect_state",
		Value:    stateValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	// Track if the enable was initiated from an external client app so we can
	// show a "you can close this window" message after the OAuth flow completes.
	// External if: ?external=1, no referrer (direct navigation / external link),
	// or referrer from a different host.
	external := c.QueryParam("external") == "1"
	if !external {
		referer := r.Header.Get("Referer")
		if referer == "" {
			external = true
		} else if refURL, err := url.Parse(referer); err == nil && refURL.Host != "" && refURL.Host != r.Host {
			external = true
		}
	}
	if external {
		c.SetCookie(&http.Cookie{
			Name:     "connect_external",
			Value:    "1",
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   600,
		})
	}

	opts := []oauth2.AuthCodeOption{oauth2.AccessTypeOffline}
	if googleServices[service] {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "consent"))
	}
	if service == "slack" {
		opts = append(opts, oauth2.SetAuthURLParam("user_scope", strings.Join(cfg.Scopes, ",")))
		cfg.Scopes = nil
	}

	return c.Redirect(http.StatusFound, cfg.AuthCodeURL(state, opts...))
}

func (s *Server) HandleConnectCallback(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	stateCookie, err := r.Cookie("connect_state")
	if err != nil || stateCookie.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing state cookie")
	}
	c.SetCookie(&http.Cookie{
		Name:     "connect_state",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	parts := strings.SplitN(stateCookie.Value, ":", 3)
	if len(parts) != 3 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid state cookie")
	}
	service, level, expectedState := parts[0], parts[1], parts[2]

	if c.QueryParam("state") != expectedState {
		return echo.NewHTTPError(http.StatusBadRequest, "state mismatch")
	}

	if errParam := c.QueryParam("error"); errParam != "" {
		slog.Warn("oauth denied", "service", service, "error", errParam)
		return c.Redirect(http.StatusFound, "/connect/"+service)
	}

	cfg, err := s.oauthConfigForService(r, service, level)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	slog.Info("oauth exchange attempt", "service", service, "redirect_url", cfg.RedirectURL)

	var accessToken, refreshToken string
	var expiry time.Time

	if service == "slack" {
		at, err := s.slackTokenExchange(ctx, cfg, c.QueryParam("code"))
		if err != nil {
			slog.Error("slack exchange", "error", err)
			return echo.NewHTTPError(http.StatusBadRequest, "failed to exchange code")
		}
		accessToken = at
	} else {
		token, err := cfg.Exchange(ctx, c.QueryParam("code"))
		if err != nil {
			slog.Error("oauth exchange", "service", service, "redirect_url", cfg.RedirectURL, "error", err)
			return echo.NewHTTPError(http.StatusBadRequest, "failed to exchange code")
		}
		accessToken = token.AccessToken
		refreshToken = token.RefreshToken
		expiry = token.Expiry
	}

	provider := "google"
	if !googleServices[service] {
		provider = service
	}

	svc := servicesByID[service]
	var scopeStr string
	for _, c := range svc.Connections {
		if c.Level == level {
			scopeStr = strings.Join(c.Scopes, ",")
			break
		}
	}

	var expiresAt *time.Time
	if !expiry.IsZero() {
		expiresAt = &expiry
	}

	if s.DB == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "database not configured")
	}

	q := dbgen.New(s.DB)
	if err := q.UpsertOAuthToken(ctx, dbgen.UpsertOAuthTokenParams{
		AccessToken:  accessToken,
		ExpiresAt:    expiresAt,
		Level:        level,
		Provider:     provider,
		RefreshToken: refreshToken,
		Scopes:       scopeStr,
		Service:      service,
		Subject:     subject,
	}); err != nil {
		return errors.Wrap(err, "save oauth token")
	}

	meta := userEmail + " connected " + svc.Name + " (" + level + ")"
	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    subject,
		ActorType:  "user",
		Action:     "connected_integration",
		ObjectID:   service,
		ObjectType: "integration",
		Metadata:   &meta,
	})

	slog.Info("oauth connected", "service", service, "level", level, "user", userEmail)
	return c.Redirect(http.StatusFound, "/connect/"+service+"?connected=1")
}

func (s *Server) slackTokenExchange(ctx context.Context, cfg *oauth2.Config, code string) (string, error) {
	data := url.Values{
		"client_id":     {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
		"code":          {code},
		"redirect_uri":  {cfg.RedirectURL},
	}
	resp, err := http.PostForm(cfg.Endpoint.TokenURL, data)
	if err != nil {
		return "", errors.Wrap(err, "slack token request")
	}
	defer resp.Body.Close()

	var result struct {
		OK         bool   `json:"ok"`
		Error      string `json:"error"`
		AuthedUser struct {
			AccessToken string `json:"access_token"`
		} `json:"authed_user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", errors.Wrap(err, "decode slack response")
	}
	if !result.OK {
		return "", errors.Newf("slack error: %s", result.Error)
	}
	if result.AuthedUser.AccessToken == "" {
		return "", errors.New("no user access token in slack response")
	}
	return result.AuthedUser.AccessToken, nil
}

func (s *Server) HandleConnectDisconnect(c echo.Context) error {
	ctx := c.Request().Context()
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)
	service := c.Param("service")
	level := c.Param("level")

	svc, ok := servicesByID[service]
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "unknown service")
	}

	if s.DB == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "database not configured")
	}

	q := dbgen.New(s.DB)
	if err := q.DeleteOAuthToken(ctx, dbgen.DeleteOAuthTokenParams{
		Subject: subject,
		Service: service,
		Level:   level,
	}); err != nil {
		return errors.Wrap(err, "delete oauth token")
	}

	meta := userEmail + " disconnected " + svc.Name + " (" + level + ")"
	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    subject,
		ActorType:  "user",
		Action:     "disconnected_integration",
		ObjectID:   service,
		ObjectType: "integration",
		Metadata:   &meta,
	})

	slog.Info("oauth disconnected", "service", service, "level", level, "user", userEmail)
	return c.Redirect(http.StatusFound, "/connect/"+service)
}

func (s *Server) refreshOAuthToken(ctx context.Context, tok dbgen.OauthToken) (string, error) {
	var endpoint oauth2.Endpoint
	var clientID, clientSecret string

	switch tok.Provider {
	case "google":
		endpoint = oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		}
		clientID = s.OAuth.ClientID
		clientSecret = s.OAuth.ClientSecret
	case "slack":
		return "", errors.New("slack tokens do not support refresh")
	case "attio":
		endpoint = oauth2.Endpoint{
			TokenURL:  "https://app.attio.com/oidc/token",
			AuthStyle: oauth2.AuthStyleInParams,
		}
		clientID = tok.ClientID
		clientSecret = ""
	case "notion":
		endpoint = oauth2.Endpoint{
			TokenURL:  "https://mcp.notion.com/token",
			AuthStyle: oauth2.AuthStyleInParams,
		}
		clientID = tok.ClientID
		clientSecret = ""
	case "granola":
		endpoint = oauth2.Endpoint{
			TokenURL:  "https://mcp-auth.granola.ai/oauth2/token",
			AuthStyle: oauth2.AuthStyleInParams,
		}
		clientID = tok.ClientID
		clientSecret = ""
	default:
		return "", errors.Newf("unknown provider for refresh: %s", tok.Provider)
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

	newToken, err := cfg.TokenSource(ctx, oldToken).Token()
	if err != nil {
		return "", errors.Wrap(err, "refresh token")
	}

	var expiresAt *time.Time
	if !newToken.Expiry.IsZero() {
		expiresAt = &newToken.Expiry
	}

	refreshToken := newToken.RefreshToken
	if refreshToken == "" {
		refreshToken = tok.RefreshToken
	}

	q := dbgen.New(s.DB)
	if err := q.UpsertOAuthToken(ctx, dbgen.UpsertOAuthTokenParams{
		AccessToken:  newToken.AccessToken,
		ClientID:     tok.ClientID,
		ExpiresAt:    expiresAt,
		Level:        tok.Level,
		Provider:     tok.Provider,
		RefreshToken: refreshToken,
		Scopes:       tok.Scopes,
		Service:      tok.Service,
		Subject:     tok.Subject,
	}); err != nil {
		slog.Warn("failed to persist refreshed token", "service", tok.Service, "error", err)
	}

	slog.Info("oauth token refreshed", "service", tok.Service, "level", tok.Level, "subject", tok.Subject)
	return newToken.AccessToken, nil
}
