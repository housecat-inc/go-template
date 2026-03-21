package srv

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	googleoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/oauth2"

	"github.com/housecat-inc/auth/assets"
	"github.com/housecat-inc/auth/gcpdns"
	hcmcp "github.com/housecat-inc/auth/mcp"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/housecat-inc/auth/db"
	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/exedev"
	"github.com/housecat-inc/auth/gh"
	hcoidc "github.com/housecat-inc/auth/oidc"
	"github.com/housecat-inc/auth/ui/blocks/auth"
	"github.com/housecat-inc/auth/ui/pages"
)

type vmSetup struct {
	Done       bool
	ShelleyURL string
}

type ServiceOAuthConfig struct {
	ClientID     string
	ClientSecret string
}

type Server struct {
	DB     *sql.DB
	DNS    *gcpdns.Client
	ExeDev *exedev.Client
	GitProxy *gh.Proxy
	Hostname          string
	HostnameAliases   []string
	OAuth             OAuthConfig
	SlackOAuth        ServiceOAuthConfig
	oauth2Config  *oauth2.Config
	googleOIDC    *googleoidc.Provider
	oidcCrypto    op.Crypto
	oidcOP        op.OpenIDProvider
	oidcStorage   *hcoidc.Storage
	sessionSecret string
	vmSetups      sync.Map
}

func New(dbPath, hostname string, hostnameAliases []string, oauthCfg OAuthConfig, exedevKeyPath string) (*Server, error) {
	srv := &Server{
		Hostname:        hostname,
		HostnameAliases: hostnameAliases,
		sessionSecret:   oauthCfg.SessionSecret,
		OAuth:           oauthCfg,
	}
	if err := srv.setUpDatabase(dbPath); err != nil {
		return nil, err
	}
	if err := srv.setUpOIDCProvider(dbPath); err != nil {
		return nil, err
	}
	if oauthCfg.ClientID != "" {
		if err := srv.setUpGoogleOIDC(context.Background()); err != nil {
			return nil, errors.Wrap(err, "setup google oidc")
		}
	}
	if exedevKeyPath != "" {
		client, err := exedev.New(exedevKeyPath)
		if err != nil {
			slog.Warn("exe.dev client disabled", "error", err)
		} else {
			srv.ExeDev = client
		}
	}
	return srv, nil
}

func (s *Server) setUpOIDCProvider(dbPath string) error {
	issuer := "https://" + s.Hostname
	if strings.HasPrefix(s.Hostname, "localhost") {
		issuer = "http://" + s.Hostname
	}

	allowedIssuers := map[string]string{s.Hostname: issuer}
	for _, alias := range s.HostnameAliases {
		allowedIssuers[alias] = "https://" + alias
	}

	keyPath := filepath.Join(filepath.Dir(dbPath), "oidc_signing.key")
	provider, storage, crypto, err := hcoidc.NewProvider(issuer, allowedIssuers, s.DB, keyPath, s.sessionSecret)
	if err != nil {
		return errors.Wrap(err, "setup oidc provider")
	}
	s.oidcOP = provider
	s.oidcStorage = storage
	s.oidcCrypto = crypto

	slog.Info("oidc provider initialized", "issuer", issuer)
	return nil
}

func (s *Server) Serve(addr string) error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			if err != nil {
				c.Error(err)
			}
			req := c.Request()
			res := c.Response()
			path := req.URL.Path
			if path == "/healthz" || path == "/ready" || strings.HasPrefix(path, "/assets/") {
				return nil
			}
			latency := time.Since(start)
			attrs := []any{
				"method", req.Method,
				"path", path,
				"status", res.Status,
				"latency", latency.Round(time.Millisecond).String(),
			}
			if q := req.URL.RawQuery; q != "" {
				attrs = append(attrs, "query", q)
			}
			if res.Status >= 400 {
				slog.Warn("http request", attrs...)
			} else {
				slog.Info("http request", attrs...)
			}
			return nil
		}
	})

	if s.Hostname != "" {
		aliasHosts := map[string]bool{}
		for _, alias := range s.HostnameAliases {
			aliasHosts[alias] = true
		}
		e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				host := c.Request().Host
				if host == s.Hostname || host == "localhost:8000" {
					return next(c)
				}
				if aliasHosts[host] {
					path := c.Request().URL.Path
					if strings.HasPrefix(path, "/.well-known/") ||
						strings.HasPrefix(path, "/authorize") ||
						strings.HasPrefix(path, "/oauth/") ||
						path == "/keys" ||
						path == "/userinfo" ||
						strings.HasPrefix(path, "/oidc/") ||
						strings.HasPrefix(path, "/api/") ||
						strings.HasPrefix(path, "/register") ||
						strings.HasPrefix(path, "/mcp") ||
						strings.HasPrefix(path, "/github.com/") ||
						strings.HasPrefix(path, "/api.github.com/") ||
						strings.HasPrefix(path, "/gh/") ||
						strings.HasPrefix(path, "/gitproxy/") {
						return next(c)
					}
				}
				target := "https://" + s.Hostname + c.Request().RequestURI
				return c.Redirect(http.StatusMovedPermanently, target)
			}
		})
	}

	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		code := http.StatusInternalServerError
		msg := "internal server error"
		var he *echo.HTTPError
		if errors.As(err, &he) {
			code = he.Code
			if m, ok := he.Message.(string); ok {
				msg = m
			}
		}
		slog.Error("http error",
			"status", code,
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
			"error", err,
		)
		_ = c.String(code, msg)
	}

	e.GET("/", s.HandleRoot)
	e.GET("/connect", s.HandleHome, s.RequireAuth)
	e.GET("/connect/:service", s.HandleConnect, s.RequireAuth)
	e.GET("/connect/:service/enable/:level", s.HandleConnectEnable, s.RequireAuth)
	e.POST("/connect/:service/disconnect/:level", s.HandleConnectDisconnect, s.RequireAuth)

	e.GET("/settings", s.HandleSettings, s.RequireAuth)
	e.POST("/settings", s.HandleSettingsUpdate, s.RequireAuth)
	e.GET("/profile", s.HandleProfile, s.RequireAuth)
	e.GET("/auth/exedev", s.HandleAuthExeDev)
	if s.oauth2Config != nil {
		e.GET("/auth/google", s.HandleAuthGoogle)
		e.GET("/auth/google/callback", s.handleAuthLoginCallback)
		e.GET("/connect/google/callback", s.HandleConnectCallback, s.RequireAuth)
	}
	e.GET("/connect/attio/callback", s.HandleAttioCallback, s.RequireAuth)
	e.GET("/connect/granola/callback", s.HandleGranolaCallback, s.RequireAuth)
	e.GET("/connect/slack/callback", s.HandleConnectCallback, s.RequireAuth)
	e.GET("/connect/notion/callback", s.HandleNotionCallback, s.RequireAuth)
	e.GET("/auth/logout", s.HandleAuthLogout)

	admin := e.Group("/admin", s.RequireAuth, s.RequireAdmin)
	admin.GET("/vms", s.HandleAdminVMs)
	admin.POST("/vms/new", s.HandleAdminNewVM)
	admin.GET("/vms/:name/setup", s.HandleAdminVMSetup)
	admin.GET("/vms/:name/setup/status", s.HandleAdminVMSetupStatus)
	admin.POST("/vms/:name/delete", s.HandleAdminDeleteVM)
	admin.POST("/vms/:name/share", s.HandleAdminToggleShare)
	admin.GET("/open", s.HandleAdminOpenVM)
	admin.GET("/resolve-branch", s.HandleResolveBranch)

	clients := admin.Group("/clients")
	clients.GET("", s.HandleClients)
	clients.GET("/new", s.HandleClientsNew)
	clients.POST("", s.HandleClientsCreate)
	clients.POST("/registration-token", s.HandleRegistrationToken)
	clients.GET("/:id", s.HandleClientsView)
	clients.GET("/:id/edit", s.HandleClientsEdit)
	clients.POST("/:id", s.HandleClientsUpdate)
	clients.POST("/:id/archive", s.HandleClientsArchive)

	e.POST("/api/register", s.HandleRegister)
	e.POST("/register", s.HandleMCPRegister)

	e.GET("/gitproxy/ca.crt", s.HandleGitProxyProbe)

	if s.GitProxy != nil {
		gp := echo.WrapHandler(s.GitProxy)
		e.Any("/github.com/*", gp)
		e.Any("/api.github.com/*", gp)
		e.GET("/gh/token", echo.WrapHandler(http.HandlerFunc(s.GitProxy.HandleToken)))
	}

	// OIDC login callback (the OP redirects here for user authentication)
	e.GET("/oidc/login", hcoidc.LoginHandler(s.oidcStorage, s.oidcOP, func(r *http.Request) (string, string, error) {
		sess, err := s.getSession(r)
		if err != nil {
			return "", "", err
		}
		return sess.Subject, sess.Email, nil
	}))

	// OAuth Protected Resource Metadata (RFC 9728) for MCP
	issuerURL := "https://" + s.Hostname
	prmMetadata := &oauthex.ProtectedResourceMetadata{
		Resource:             issuerURL + "/mcp",
		AuthorizationServers: []string{issuerURL},
		ScopesSupported:      []string{"openid", "email", "profile"},
	}
	e.Any("/.well-known/oauth-protected-resource", echo.WrapHandler(mcpauth.ProtectedResourceMetadataHandler(prmMetadata)))
	e.Any("/.well-known/oauth-protected-resource/*", echo.WrapHandler(mcpauth.ProtectedResourceMetadataHandler(prmMetadata)))

	// Mount the zitadel OIDC provider handler for all OIDC endpoints
	oidcHandler := s.oidcOP
	e.GET("/.well-known/openid-configuration", s.handleDiscoveryWithRegistration(oidcHandler))
	e.Any("/.well-known/*", echo.WrapHandler(oidcHandler))
	e.Any("/authorize", echo.WrapHandler(oidcHandler))
	e.Any("/authorize/*", echo.WrapHandler(oidcHandler))
	e.Any("/oauth/*", echo.WrapHandler(oidcHandler))
	e.Any("/userinfo", echo.WrapHandler(oidcHandler))
	e.Any("/keys", echo.WrapHandler(oidcHandler))
	e.Any("/end_session", echo.WrapHandler(oidcHandler))
	e.Any("/revoke", echo.WrapHandler(oidcHandler))
	e.Any("/healthz", echo.WrapHandler(oidcHandler))
	e.Any("/ready", echo.WrapHandler(oidcHandler))
	e.Any("/device_authorization", echo.WrapHandler(oidcHandler))

	mcpServer := hcmcp.NewServer(
		"https://"+s.Hostname,
		func(ctx context.Context, subject, service, level string) (string, error) {
			if s.DB == nil {
				return "", errors.New("database not configured")
			}
			q := dbgen.New(s.DB)
			tok, err := q.GetOAuthToken(ctx, dbgen.GetOAuthTokenParams{
				Subject: subject,
				Service: service,
				Level:   level,
			})
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return "", hcmcp.ErrTokenNotFound
				}
				return "", errors.Wrap(err, "get oauth token")
			}
			if tok.ExpiresAt != nil && tok.ExpiresAt.Before(time.Now().Add(30*time.Second)) && tok.RefreshToken != "" {
				refreshed, err := s.refreshOAuthToken(ctx, tok)
				if err != nil {
					slog.Warn("oauth token refresh failed", "service", service, "level", level, "subject", subject, "error", err)
					return tok.AccessToken, nil
				}
				return refreshed, nil
			}
			return tok.AccessToken, nil
		},
		func(ctx context.Context, userID string) map[string]map[string]bool {
			return s.connectedLevelsForSubject(ctx, userID)
		},
		func(ctx context.Context, subject string) bool {
			if s.DB == nil {
				return true
			}
			q := dbgen.New(s.DB)
			row, err := q.GetUserSettings(ctx, subject)
			if err != nil {
				return DefaultSettings.BrandingFooter
			}
			return parseSettings(row.Settings).BrandingFooter
		},
		hcmcp.DefaultUpstreamTools(),
	)
	mcpHandler := gomcp.NewStreamableHTTPHandler(func(req *http.Request) *gomcp.Server {
		return mcpServer
	}, &gomcp.StreamableHTTPOptions{
		DisableLocalhostProtection: true,
		Stateless:                  true,
	})
	mcpAuth := mcpauth.RequireBearerToken(s.verifyMCPToken, &mcpauth.RequireBearerTokenOptions{
		ResourceMetadataURL: issuerURL + "/.well-known/oauth-protected-resource",
	})
	e.Any("/mcp", echo.WrapHandler(mcpAuth(mcpHandler)))

	assetHandler := http.FileServer(http.FS(assets.FS))
	e.GET("/assets/*", echo.WrapHandler(http.StripPrefix("/assets", assetHandler)))

	slog.Info("starting server", "addr", addr)
	return e.Start(addr)
}

func (s *Server) HandleRoot(c echo.Context) error {
	r := c.Request()

	if isLoopback(r) {
		return c.Redirect(http.StatusFound, "/connect")
	}

	if _, err := s.getSession(r); err == nil {
		return c.Redirect(http.StatusFound, "/connect")
	}

	redirect := c.QueryParam("redirect")

	googleURL := ""
	if s.oauth2Config != nil {
		googleURL = "/auth/google"
		if redirect != "" {
			googleURL += "?redirect=" + url.QueryEscape(redirect)
		}
	}

	loginURL := "/auth/exedev"
	if redirect != "" {
		loginURL += "?redirect=" + url.QueryEscape(redirect)
	}
	component := auth.SignInPage(loginURL, googleURL)
	return component.Render(r.Context(), c.Response())
}

func (s *Server) HandleProfile(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)
	provider := c.Get("provider").(string)

	var activityCount int64
	var sessionCount int64
	var sessionStart time.Time
	var activities []pages.ActivityEntry

	if s.DB != nil {
		q := dbgen.New(s.DB)

		_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
			ActorID:   subject,
			ActorType: "user",
			Action:    "page_view",
		})

		if n, err := q.CountActivitiesByActor(ctx, subject); err == nil {
			activityCount = n
		}

		count, err := q.CountSessionsBySubject(ctx, subject)
		if err == nil {
			sessionCount = count
		}

		if cookie, err := r.Cookie("session_id"); err == nil {
			if sess, err := q.GetSession(ctx, cookie.Value); err == nil {
				sessionStart = sess.CreatedAt
			}
		}

		dbActivities, err := q.ListActivitiesByActor(ctx, dbgen.ListActivitiesByActorParams{
			ActorID: subject,
			Limit:   10,
		})
		if err == nil {
			for _, a := range dbActivities {
				meta := ""
				if a.Metadata != nil {
					meta = *a.Metadata
				}
				activities = append(activities, pages.ActivityEntry{
					Action:    a.Action,
					CreatedAt: a.CreatedAt,
					Metadata:  meta,
				})
			}
		}
	}

	data := pages.PageData{
		Activities:    activities,
		ActivityCount: activityCount,
		LogoutURL:     logoutURL,
		Provider:      provider,
		SessionCount:  sessionCount,
		SessionStart:  sessionStart,
		Subject:       subject,
		UserEmail:     userEmail,
	}

	component := pages.Profile(data, isAdminWithProvider(userEmail, provider))
	return component.Render(ctx, c.Response())
}

func (s *Server) setUpDatabase(dbPath string) error {
	wdb, err := db.Open(dbPath)
	if err != nil {
		return errors.Wrap(err, "open db")
	}
	s.DB = wdb
	if err := db.RunMigrations(wdb); err != nil {
		return errors.Wrap(err, "run migrations")
	}
	return nil
}



func (s *Server) issuerURL(r *http.Request) string {
	if s.Hostname != "" {
		scheme := "https"
		if strings.HasPrefix(s.Hostname, "localhost") {
			scheme = "http"
		}
		return scheme + "://" + s.Hostname
	}

	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		if idx := strings.IndexByte(proto, ','); idx != -1 {
			proto = proto[:idx]
		}
		proto = strings.TrimSpace(proto)
		if proto == "http" || proto == "https" {
			scheme = proto
		}
	} else if r.TLS == nil && strings.HasPrefix(r.Host, "localhost") {
		scheme = "http"
	}

	host := r.Host
	if xfHost := r.Header.Get("X-Forwarded-Host"); xfHost != "" {
		if idx := strings.IndexByte(xfHost, ','); idx != -1 {
			xfHost = xfHost[:idx]
		}
		xfHost = strings.TrimSpace(xfHost)
		if xfHost != "" {
			host = xfHost
		}
	}

	return scheme + "://" + host
}

func mainDomainFromHost(host string) string {
	portSuffix := ""
	if i := strings.LastIndex(host, ":"); i != -1 {
		portSuffix = host[i:]
		host = host[:i]
	}
	parts := strings.Split(host, ".")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, ".") + portSuffix
}
