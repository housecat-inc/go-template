package srv

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	"github.com/housecat-inc/auth/assets"
	"github.com/housecat-inc/auth/db"
	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/exedev"
	"github.com/housecat-inc/auth/ui/blocks/auth"
	"github.com/housecat-inc/auth/ui/pages"
)

type Server struct {
	DB            *sql.DB
	ExeDev        *exedev.Client
	Hostname      string
	OAuth         OAuthConfig
	oauth2Config  *oauth2.Config
	oidcProvider  *oidc.Provider
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
	if oauthCfg.ClientID != "" {
		if err := srv.setUpOIDC(context.Background()); err != nil {
			return nil, errors.Wrap(err, "setup oidc")
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
	admin.POST("/browser-link", s.HandleAdminBrowserLink)

	assetHandler := http.FileServer(http.FS(assets.FS))
	e.GET("/assets/*", echo.WrapHandler(http.StripPrefix("/assets", assetHandler)))

	slog.Info("starting server", "addr", addr)
	return e.Start(addr)
}

func (s *Server) HandleRoot(c echo.Context) error {
	r := c.Request()

	// On localhost, skip sign-in — RequireAuth on /home will auto-authenticate
	if strings.HasPrefix(r.Host, "localhost") {
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

	component := pages.Home(data, isAdmin(userEmail))
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
