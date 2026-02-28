package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/lmittmann/tint"

	"srv.housecat.com/srv"
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
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	oauthCfg := srv.OAuthConfig{
		ClientID:      os.Getenv("HOUSECAT_CLIENT_ID"),
		ClientSecret:  os.Getenv("HOUSECAT_CLIENT_SECRET"),
		Issuer:        os.Getenv("OAUTH_ISSUER"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
	}
	if oauthCfg.Issuer == "" && oauthCfg.ClientID != "" {
		oauthCfg.Issuer = "https://auth.housecat.com"
	}

	server, err := srv.New("db.sqlite3", hostname, oauthCfg)
	if err != nil {
		return errors.Wrap(err, "create server")
	}
	return server.Serve(*flagListenAddr)
}
