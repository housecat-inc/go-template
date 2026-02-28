package srv

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"

	"srv.housecat.com/db"
	"srv.housecat.com/db/dbgen"
	"srv.housecat.com/ui/blocks/auth"
	"srv.housecat.com/ui/pages"
)

type Server struct {
	AssetsDir    string
	DB           *sql.DB
	Hostname     string
	OAuth        OAuthConfig
	oauth2Config *oauth2.Config
	oidcProvider *oidc.Provider
}

func New(dbPath, hostname string, oauthCfg OAuthConfig) (*Server, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	srv := &Server{
		AssetsDir: filepath.Join(baseDir, "..", "assets"),
		Hostname:  hostname,
		OAuth:     oauthCfg,
	}
	if err := srv.setUpDatabase(dbPath); err != nil {
		return nil, err
	}
	if oauthCfg.ClientID != "" {
		if err := srv.setUpOIDC(context.Background()); err != nil {
			return nil, errors.Wrap(err, "setup oidc")
		}
	}
	return srv, nil
}

func (s *Server) Serve(addr string) error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.GET("/", s.HandleRoot)
	e.GET("/chat", s.HandleChat, s.RequireAuth)
	e.GET("/chats", s.HandleChats, s.RequireAuth)
	e.GET("/home", s.HandleHome, s.RequireAuth)
	if s.oauth2Config != nil {
		e.GET("/auth/google", s.HandleAuthGoogle)
		e.GET("/auth/callback", s.HandleAuthCallback)
	}
	e.POST("/api/chat", s.HandleChatSend, s.RequireAuth)
	e.DELETE("/api/chat/:id", s.HandleChatDelete, s.RequireAuth)
	e.GET("/auth/logout", s.HandleAuthLogout)
	e.Static("/assets", s.AssetsDir)

	slog.Info("starting server", "addr", addr)
	return e.Start(addr)
}

func (s *Server) HandleRoot(c echo.Context) error {
	r := c.Request()
	googleURL := ""
	if s.oauth2Config != nil {
		googleURL = "/auth/google"
	}
	component := auth.SignInPage(loginURLForRequest(r), googleURL)
	return component.Render(r.Context(), c.Response())
}

func (s *Server) HandleHome(c echo.Context) error {
	r := c.Request()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

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
		ActivityCount: count,
		Headers:       buildHeaderEntries(r),
		Hostname:      s.Hostname,
		LoginURL:      loginURLForRequest(r),
		LogoutURL:     logoutURL,
		Now:           now.Format(time.RFC3339),
		UserEmail:     userEmail,
	}

	component := pages.Home(data)
	return component.Render(r.Context(), c.Response())
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
