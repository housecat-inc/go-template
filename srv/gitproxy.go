package srv

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/gh"
)

var _ gh.PolicyStore = (*DBPolicyStore)(nil)

const gitScope = "git"

type DBPolicyStore struct {
	AllowedRepos []string
	DB           *sql.DB
	Ops          []gh.Op
}

func (s *DBPolicyStore) Lookup(ctx context.Context, proxyAuth string) (*gh.Policy, error) {
	clientID, clientSecret, ok := strings.Cut(proxyAuth, ":")
	if !ok {
		return nil, errors.New("invalid proxy auth")
	}

	q := dbgen.New(s.DB)
	client, err := q.GetOidcClientByClientID(ctx, clientID)
	if err != nil {
		return nil, errors.Wrap(err, "unknown client")
	}
	if client.ClientSecret != clientSecret {
		return nil, errors.New("invalid client secret")
	}
	if !hasScope(client.Scopes, gitScope) {
		return nil, errors.New("client does not have git scope")
	}

	return &gh.Policy{
		AllowedOps:     s.Ops,
		AllowedRepos:   s.AllowedRepos,
		BranchPrefixes: []string{client.Name + "/*"},
	}, nil
}

func (s *Server) HandleGitProxyProbe(c echo.Context) error {
	if s.GitProxy == nil {
		return echo.NewHTTPError(http.StatusNotFound, "git proxy not configured")
	}
	return c.String(http.StatusOK, "git proxy enabled")
}
