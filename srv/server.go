package srv

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	googleoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/oauth2"

	"github.com/housecat-inc/auth/assets"
	"github.com/housecat-inc/auth/db"
	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/exedev"
	hcoidc "github.com/housecat-inc/auth/oidc"
	"github.com/housecat-inc/auth/ui/blocks/auth"
	"github.com/housecat-inc/auth/ui/pages"
)

type Server struct {
	DB            *sql.DB
	ExeDev        *exedev.Client
	GitProxy   http.Handler
	Hostname   string
	OAuth         OAuthConfig
	oauth2Config  *oauth2.Config
	googleOIDC    *googleoidc.Provider
	oidcOP        op.OpenIDProvider
	oidcStorage   *hcoidc.Storage
	sessionSecret string
}

func New(dbPath, hostname string, oauthCfg OAuthConfig, exedevKeyPath string) (*Server, error) {
	srv := &Server{
		Hostname:      hostname,
		sessionSecret: oauthCfg.SessionSecret,
		OAuth:    oauthCfg,
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

	keyPath := filepath.Join(filepath.Dir(dbPath), "oidc_signing.key")
	provider, storage, err := hcoidc.NewProvider(issuer, s.DB, keyPath, s.sessionSecret)
	if err != nil {
		return errors.Wrap(err, "setup oidc provider")
	}
	s.oidcOP = provider
	s.oidcStorage = storage

	slog.Info("oidc provider initialized", "issuer", issuer)
	return nil
}

func (s *Server) Serve(addr string) error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.GET("/", s.HandleRoot)
	e.GET("/home", s.HandleHome, s.RequireAuth)
	e.GET("/auth/exedev", s.HandleAuthExeDev)
	if s.oauth2Config != nil {
		e.GET("/auth/google", s.HandleAuthGoogle)
		e.GET("/auth/google/callback", s.HandleAuthCallback)
	}
	e.GET("/auth/logout", s.HandleAuthLogout)

	admin := e.Group("/admin", s.RequireAuth, s.RequireAdmin)
	admin.GET("/vms", s.HandleAdminVMs)
	admin.POST("/vms/new", s.HandleAdminNewVM)
	admin.POST("/browser-link", s.HandleAdminBrowserLink)

	clients := admin.Group("/clients")
	clients.GET("", s.HandleClients)
	clients.GET("/new", s.HandleClientsNew)
	clients.POST("", s.HandleClientsCreate)
	clients.POST("/registration-token", s.HandleRegistrationToken)
	clients.GET("/:id", s.HandleClientsView)
	clients.GET("/:id/edit", s.HandleClientsEdit)
	clients.POST("/:id", s.HandleClientsUpdate)
	clients.POST("/:id/archive", s.HandleClientsArchive)

	e.POST("/register", s.HandleRegister)

	e.GET("/gitproxy/ca.crt", s.HandleGitProxyProbe)

	if s.GitProxy != nil {
		gp := echo.WrapHandler(s.GitProxy)
		e.Any("/github.com/*", gp)
		e.Any("/api.github.com/*", gp)
	}

	// OIDC login callback (the OP redirects here for user authentication)
	e.GET("/oidc/login", hcoidc.LoginHandler(s.oidcStorage, s.oidcOP, func(r *http.Request) (string, string, error) {
		sess, err := s.getSession(r)
		if err != nil {
			return "", "", err
		}
		return sess.UserID, sess.Email, nil
	}))

	// Mount the zitadel OIDC provider handler for all OIDC endpoints
	oidcHandler := s.oidcOP
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

	assetHandler := http.FileServer(http.FS(assets.FS))
	e.GET("/assets/*", echo.WrapHandler(http.StripPrefix("/assets", assetHandler)))

	slog.Info("starting server", "addr", addr)
	return e.Start(addr)
}

func (s *Server) HandleRoot(c echo.Context) error {
	r := c.Request()

	if isLoopback(r) {
		return c.Redirect(http.StatusFound, "/home")
	}

	if _, err := s.getSession(r); err == nil {
		return c.Redirect(http.StatusFound, "/home")
	}

	googleURL := ""
	if s.oauth2Config != nil {
		googleURL = "/auth/google"
	}
	component := auth.SignInPage("/auth/exedev", googleURL)
	return component.Render(r.Context(), c.Response())
}

func (s *Server) HandleHome(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userID := c.Get("userID").(string)
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
			ActorID:   userID,
			ActorType: "user",
			Action:    "page_view",
		})

		if n, err := q.CountActivitiesByActor(ctx, userID); err == nil {
			activityCount = n
		}

		count, err := q.CountSessionsByUser(ctx, userID)
		if err == nil {
			sessionCount = count
		}

		if cookie, err := r.Cookie("session_id"); err == nil {
			if sess, err := q.GetSession(ctx, cookie.Value); err == nil {
				sessionStart = sess.CreatedAt
			}
		}

		dbActivities, err := q.ListActivitiesByActor(ctx, dbgen.ListActivitiesByActorParams{
			ActorID: userID,
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
		UserEmail:     userEmail,
		UserID:        userID,
	}

	component := pages.Home(data, isAdminWithProvider(userEmail, provider))
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
