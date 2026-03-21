package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/cockroachdb/errors"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// NewProvider creates an OIDC provider backed by the given database and signing key path.
// allowedIssuers maps hostnames to their full issuer URLs. Requests from unrecognized
// hosts fall back to the primary issuer.
func NewProvider(primaryIssuer string, allowedIssuers map[string]string, db *sql.DB, keyPath string, sessionSecret string) (op.OpenIDProvider, *Storage, op.Crypto, error) {
	key, err := LoadOrGenerateSigningKey(keyPath)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "load signing key")
	}

	loginURL := primaryIssuer + "/oidc/login"
	storage := NewStorage(db, key, loginURL)

	if sessionSecret == "" {
		slog.Warn("SESSION_SECRET is empty, generating random OIDC crypto key (tokens will not survive restarts)")
		var randomKey [32]byte
		if _, err := rand.Read(randomKey[:]); err != nil {
			return nil, nil, nil, errors.Wrap(err, "generate random crypto key")
		}
		sessionSecret = string(randomKey[:])
	}
	cryptoKey := sha256.Sum256([]byte(sessionSecret))
	config := &op.Config{
		AuthMethodPost:        true,
		CodeMethodS256:        true,
		CryptoKey:             cryptoKey,
		GrantTypeRefreshToken: true,
	}

	issuerFunc := func(insecure bool) (op.IssuerFromRequest, error) {
		return func(r *http.Request) string {
			if iss, ok := allowedIssuers[r.Host]; ok {
				return iss
			}
			return primaryIssuer
		}, nil
	}

	provider, err := op.NewProvider(config, storage, issuerFunc)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "create oidc provider")
	}

	return provider, storage, op.NewAESCrypto(cryptoKey), nil
}
