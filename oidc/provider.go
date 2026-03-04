package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"log/slog"

	"github.com/cockroachdb/errors"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// NewProvider creates an OIDC provider backed by the given database and signing key path.
func NewProvider(issuer string, db *sql.DB, keyPath string, sessionSecret string) (op.OpenIDProvider, *Storage, op.Crypto, error) {
	key, err := LoadOrGenerateSigningKey(keyPath)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "load signing key")
	}

	loginURL := issuer + "/oidc/login"
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
		CryptoKey:      cryptoKey,
		CodeMethodS256: true,
		AuthMethodPost: true,
	}

	provider, err := op.NewOpenIDProvider(issuer, config, storage)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "create oidc provider")
	}

	return provider, storage, op.NewAESCrypto(cryptoKey), nil
}
