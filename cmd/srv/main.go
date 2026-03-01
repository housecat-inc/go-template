package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/lmittmann/tint"

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
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	oauthCfg := srv.OAuthConfig{
		ClientID:      os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
		Issuer:        "https://accounts.google.com",
		SessionSecret: os.Getenv("SESSION_SECRET"),
	}

	server, err := srv.New("db.sqlite3", hostname, oauthCfg)
	if err != nil {
		return errors.Wrap(err, "create server")
	}
	return server.Serve(*flagListenAddr)
}
