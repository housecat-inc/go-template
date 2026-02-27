package srv

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"srv.housecat.com/db"
	"srv.housecat.com/db/dbgen"
	"srv.housecat.com/ui/blocks/auth"
	"srv.housecat.com/ui/pages"
)

type Server struct {
	DB        *sql.DB
	Hostname  string
	AssetsDir string
}

func New(dbPath, hostname string) (*Server, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	srv := &Server{
		Hostname:  hostname,
		AssetsDir: filepath.Join(baseDir, "..", "assets"),
	}
	if err := srv.setUpDatabase(dbPath); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *Server) HandleRoot(c echo.Context) error {
	r := c.Request()
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	userEmail := strings.TrimSpace(r.Header.Get("X-ExeDev-Email"))

	if userID == "" {
		component := auth.SignInPage(loginURLForRequest(r))
		return component.Render(r.Context(), c.Response())
	}

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
		Hostname:      s.Hostname,
		Now:           now.Format(time.RFC3339),
		UserEmail:     userEmail,
		ActivityCount: count,
		LoginURL:      loginURLForRequest(r),
		LogoutURL:     "/__exe.dev/logout",
		Headers:       buildHeaderEntries(r),
	}

	component := pages.Welcome(data)
	return component.Render(r.Context(), c.Response())
}

func loginURLForRequest(r *http.Request) string {
	path := r.URL.RequestURI()
	v := url.Values{}
	v.Set("redirect", path)
	return "/__exe.dev/login?" + v.Encode()
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

func (s *Server) setUpDatabase(dbPath string) error {
	wdb, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	s.DB = wdb
	if err := db.RunMigrations(wdb); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

func (s *Server) Serve(addr string) error {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.GET("/", s.HandleRoot)
	e.Static("/assets", s.AssetsDir)

	slog.Info("starting server", "addr", addr)
	return e.Start(addr)
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
