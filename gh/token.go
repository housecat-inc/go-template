package gh

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type TokenSource struct {
	appID          int64
	httpClient     *http.Client
	installationID int64
	key            *rsa.PrivateKey

	mu    sync.Mutex
	token string
	exp   time.Time
}

func NewTokenSource(pemPath string, appID, installationID int64) (*TokenSource, error) {
	data, err := os.ReadFile(pemPath)
	if err != nil {
		return nil, errors.Wrap(err, "read pem")
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("failed to decode pem")
	}

	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, errors.Wrap(parseErr, "parse pkcs8 key")
		}
		var ok bool
		key, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("pem is not an rsa key")
		}
	default:
		return nil, errors.Newf("unexpected pem type: %s", block.Type)
	}
	if err != nil {
		return nil, errors.Wrap(err, "parse rsa key")
	}

	return &TokenSource{
		appID:          appID,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		installationID: installationID,
		key:            key,
	}, nil
}

func (ts *TokenSource) Token(ctx context.Context) (string, error) {
	ts.mu.Lock()
	if ts.token != "" && time.Now().Before(ts.exp.Add(-60*time.Second)) {
		t := ts.token
		ts.mu.Unlock()
		return t, nil
	}
	ts.mu.Unlock()

	jwtToken, err := ts.createJWT()
	if err != nil {
		return "", errors.Wrap(err, "create jwt")
	}

	token, exp, err := ts.exchangeForInstallationToken(ctx, jwtToken)
	if err != nil {
		return "", errors.Wrap(err, "exchange for installation token")
	}

	ts.mu.Lock()
	ts.token = token
	ts.exp = exp
	ts.mu.Unlock()

	return token, nil
}

func (ts *TokenSource) createJWT() (string, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: ts.key},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return "", errors.Wrap(err, "create signer")
	}

	now := time.Now()
	claims := jwt.Claims{
		Expiry:   jwt.NewNumericDate(now.Add(10 * time.Minute)),
		IssuedAt: jwt.NewNumericDate(now.Add(-60 * time.Second)),
		Issuer:   fmt.Sprintf("%d", ts.appID),
	}

	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", errors.Wrap(err, "sign jwt")
	}
	return raw, nil
}

func (ts *TokenSource) exchangeForInstallationToken(ctx context.Context, jwtToken string) (string, time.Time, error) {
	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", ts.installationID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "create request")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwtToken)

	resp, err := ts.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "do request")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "read response")
	}

	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, errors.Newf("github api %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ExpiresAt time.Time `json:"expires_at"`
		Token     string    `json:"token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", time.Time{}, errors.Wrap(err, "unmarshal response")
	}

	return result.Token, result.ExpiresAt, nil
}
