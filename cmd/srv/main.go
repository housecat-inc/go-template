package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/lmittmann/tint"

	"github.com/housecat-inc/auth/gcpdns"
	"github.com/housecat-inc/auth/gh"
	"github.com/housecat-inc/auth/srv"
)

var flagListenAddr = flag.String("listen", ":8000", "address to listen on")

func main() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		TimeFormat: time.Kitchen,
	})))

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func run() error {
	flag.Parse()
	hostname := os.Getenv("HOSTNAME")
	if hostname == "" {
		h, err := os.Hostname()
		if err != nil {
			hostname = "localhost:8000"
		} else {
			hostname = h + ".exe.xyz"
		}
	}
	oauthCfg := srv.OAuthConfig{
		ClientID:      os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
		Issuer:        "https://accounts.google.com",
		SessionSecret: os.Getenv("SESSION_SECRET"),
	}

	var aliases []string
	if v := os.Getenv("HOSTNAME_ALIASES"); v != "" {
		aliases = strings.Split(v, ",")
	}

	server, err := srv.New("db.sqlite3", hostname, aliases, oauthCfg, os.Getenv("EXEDEV_KEY_PATH"))
	if err != nil {
		return errors.Wrap(err, "create server")
	}

	server.SlackOAuth = srv.ServiceOAuthConfig{
		ClientID:     os.Getenv("SLACK_CLIENT_ID"),
		ClientSecret: os.Getenv("SLACK_CLIENT_SECRET"),
	}

	if saKeyPath := os.Getenv("GCP_DNS_SA_KEY_PATH"); saKeyPath != "" {
		dnsClient, err := gcpdns.New(saKeyPath, os.Getenv("GCP_DNS_PROJECT"), os.Getenv("GCP_DNS_ZONE"))
		if err != nil {
			slog.Warn("dns client disabled", "error", err)
		} else {
			server.DNS = dnsClient
			slog.Info("dns client configured", "project", os.Getenv("GCP_DNS_PROJECT"), "zone", os.Getenv("GCP_DNS_ZONE"))
		}
	}

	if pemPath := os.Getenv("GH_APP_PEM_PATH"); pemPath != "" {
		proxy, err := buildGitProxy(pemPath, server.DB)
		if err != nil {
			return errors.Wrap(err, "build git proxy")
		}
		server.GitProxy = proxy
	}

	return server.Serve(*flagListenAddr)
}

func buildGitProxy(pemPath string, db *sql.DB) (*gh.Proxy, error) {
	appIDStr := os.Getenv("GH_APP_ID")
	installationIDStr := os.Getenv("GH_INSTALLATION_ID")
	if appIDStr == "" || installationIDStr == "" {
		return nil, errors.New("GH_APP_ID and GH_INSTALLATION_ID required when GH_APP_PEM_PATH is set")
	}

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return nil, errors.Wrap(err, "parse GH_APP_ID")
	}
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return nil, errors.Wrap(err, "parse GH_INSTALLATION_ID")
	}

	tokenSource, err := gh.NewTokenSource(pemPath, appID, installationID)
	if err != nil {
		return nil, errors.Wrap(err, "create token source")
	}

	allowedRepos := strings.Split(os.Getenv("GH_ALLOWED_REPOS"), ",")
	allOps := []gh.Op{gh.OpFetch, gh.OpPush, gh.OpAPIRead, gh.OpAPIWrite}

	proxy := &gh.Proxy{
		DefaultPolicy: &gh.Policy{
			AllowedOps:   []gh.Op{gh.OpFetch, gh.OpAPIRead},
			AllowedRepos: allowedRepos,
		},
		PolicyStore: &srv.DBPolicyStore{
			AllowedRepos: allowedRepos,
			DB:           db,
			Ops:          allOps,
		},
		TokenSource: tokenSource,
	}

	slog.Info("git proxy configured", "repos", allowedRepos)
	return proxy, nil
}
