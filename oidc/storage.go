package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/housecat-inc/auth/db/dbgen"
)

// Storage implements op.Storage backed by SQLite.
type Storage struct {
	db       *sql.DB
	key      *ecdsa.PrivateKey
	keyID    string
	loginURL string
}

func NewStorage(db *sql.DB, key *ecdsa.PrivateKey, loginURL string) *Storage {
	pub := elliptic.Marshal(key.PublicKey.Curve, key.PublicKey.X, key.PublicKey.Y)
	hash := sha256.Sum256(pub)
	keyID := hex.EncodeToString(hash[:8])
	return &Storage{
		db:       db,
		key:      key,
		keyID:    keyID,
		loginURL: loginURL,
	}
}

func (s *Storage) q() *dbgen.Queries { return dbgen.New(s.db) }

// CreateAuthRequest stores a new auth request and returns it.
func (s *Storage) CreateAuthRequest(ctx context.Context, authReq *oidc.AuthRequest, userID string) (op.AuthRequest, error) {
	id := uuid.NewString()

	challenge := authReq.CodeChallenge
	challengeMethod := string(authReq.CodeChallengeMethod)

	err := s.q().InsertAuthRequest(ctx, dbgen.InsertAuthRequestParams{
		ID:                  id,
		ClientID:            authReq.ClientID,
		RedirectUri:         authReq.RedirectURI,
		Scopes:              strings.Join(authReq.Scopes, ","),
		State:               authReq.State,
		Nonce:               authReq.Nonce,
		ResponseType:        string(authReq.ResponseType),
		CodeChallenge:       challenge,
		CodeChallengeMethod: challengeMethod,
	})
	if err != nil {
		return nil, errors.Wrap(err, "insert auth request")
	}

	row, err := s.q().GetAuthRequest(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "get auth request")
	}
	return &AuthRequest{row}, nil
}

func (s *Storage) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	row, err := s.q().GetAuthRequest(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "get auth request")
	}
	return &AuthRequest{row}, nil
}

func (s *Storage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	row, err := s.q().GetAuthRequestByCode(ctx, code)
	if err != nil {
		return nil, errors.Wrap(err, "get auth request by code")
	}
	return &AuthRequest{row}, nil
}

func (s *Storage) SaveAuthCode(ctx context.Context, id, code string) error {
	return s.q().InsertAuthCode(ctx, dbgen.InsertAuthCodeParams{
		Code:          code,
		AuthRequestID: id,
	})
}

func (s *Storage) DeleteAuthRequest(ctx context.Context, id string) error {
	if err := s.q().DeleteAuthCodesByRequestID(ctx, id); err != nil {
		return errors.Wrap(err, "delete auth codes")
	}
	return s.q().DeleteAuthRequest(ctx, id)
}

// CreateAccessToken creates an opaque access token.
func (s *Storage) CreateAccessToken(ctx context.Context, request op.TokenRequest) (string, time.Time, error) {
	id := uuid.NewString()
	expiration := time.Now().UTC().Add(1 * time.Hour)

	err := s.q().InsertAccessToken(ctx, dbgen.InsertAccessTokenParams{
		ID:            id,
		ApplicationID: clientIDFromRequest(request),
		Subject:       request.GetSubject(),
		Audience:      strings.Join(request.GetAudience(), ","),
		Scopes:        strings.Join(request.GetScopes(), ","),
		ExpiresAt:     expiration,
	})
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "insert access token")
	}
	return id, expiration, nil
}

func (s *Storage) CreateAccessAndRefreshTokens(ctx context.Context, request op.TokenRequest, currentRefreshToken string) (string, string, time.Time, error) {
	accessTokenID, expiration, err := s.CreateAccessToken(ctx, request)
	if err != nil {
		return "", "", time.Time{}, err
	}
	// No refresh token support for now
	return accessTokenID, "", expiration, nil
}

func (s *Storage) TokenRequestByRefreshToken(ctx context.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	return nil, errors.New("refresh tokens not supported")
}

func (s *Storage) TerminateSession(ctx context.Context, userID, clientID string) error {
	return s.q().DeleteAccessTokensBySubject(ctx, dbgen.DeleteAccessTokensBySubjectParams{
		Subject:       userID,
		ApplicationID: clientID,
	})
}

func (s *Storage) RevokeToken(ctx context.Context, tokenOrTokenID, userID, clientID string) *oidc.Error {
	token, err := s.q().GetAccessToken(ctx, tokenOrTokenID)
	if err != nil {
		return nil
	}
	if clientID != "" && token.ApplicationID != clientID {
		return oidc.ErrServerError().WithDescription("token does not belong to client")
	}
	if userID != "" && token.Subject != userID {
		return oidc.ErrServerError().WithDescription("token does not belong to user")
	}
	if err := s.q().DeleteAccessToken(ctx, tokenOrTokenID); err != nil {
		return oidc.ErrServerError().WithDescription("failed to revoke token")
	}
	return nil
}

func (s *Storage) GetRefreshTokenInfo(ctx context.Context, clientID, token string) (string, string, error) {
	return "", "", op.ErrInvalidRefreshToken
}

func (s *Storage) SigningKey(ctx context.Context) (op.SigningKey, error) {
	return &signingKey{
		id:  s.keyID,
		alg: jose.ES256,
		key: s.key,
	}, nil
}

func (s *Storage) SignatureAlgorithms(ctx context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.ES256}, nil
}

func (s *Storage) KeySet(ctx context.Context) ([]op.Key, error) {
	return []op.Key{
		&publicKey{
			id:  s.keyID,
			alg: jose.ES256,
			key: &s.key.PublicKey,
		},
	}, nil
}

// OPStorage methods

func (s *Storage) GetClientByClientID(ctx context.Context, clientID string) (op.Client, error) {
	row, err := s.q().GetOidcClientByClientID(ctx, clientID)
	if err != nil {
		return nil, errors.Wrap(err, "get client")
	}
	return NewClient(row, s.loginURL), nil
}

func (s *Storage) AuthorizeClientIDSecret(ctx context.Context, clientID, clientSecret string) error {
	row, err := s.q().GetOidcClientByClientID(ctx, clientID)
	if err != nil {
		return errors.Wrap(err, "get client")
	}
	if row.ClientSecret != clientSecret {
		return errors.New("invalid client secret")
	}
	return nil
}

func (s *Storage) SetUserinfoFromScopes(ctx context.Context, userinfo *oidc.UserInfo, userID, clientID string, scopes []string) error {
	return s.setUserinfo(userinfo, userID, "", scopes)
}

func (s *Storage) SetUserinfoFromToken(ctx context.Context, userinfo *oidc.UserInfo, tokenID, subject, origin string) error {
	token, err := s.q().GetAccessToken(ctx, tokenID)
	if err != nil {
		return errors.Wrap(err, "get access token")
	}
	email := s.lookupEmail(ctx, token.Subject, token.ApplicationID)
	return s.setUserinfo(userinfo, token.Subject, email, splitComma(token.Scopes))
}

func (s *Storage) SetIntrospectionFromToken(ctx context.Context, resp *oidc.IntrospectionResponse, tokenID, subject, clientID string) error {
	token, err := s.q().GetAccessToken(ctx, tokenID)
	if err != nil {
		return errors.Wrap(err, "get access token")
	}
	resp.Active = true
	resp.Subject = token.Subject
	resp.ClientID = token.ApplicationID
	resp.Scope = oidc.SpaceDelimitedArray(splitComma(token.Scopes))
	return nil
}

func (s *Storage) GetPrivateClaimsFromScopes(ctx context.Context, userID, clientID string, scopes []string) (map[string]any, error) {
	return nil, nil
}

func (s *Storage) GetKeyByIDAndClientID(ctx context.Context, keyID, clientID string) (*jose.JSONWebKey, error) {
	return nil, errors.New("not supported")
}

func (s *Storage) ValidateJWTProfileScopes(ctx context.Context, userID string, scopes []string) ([]string, error) {
	return scopes, nil
}

func (s *Storage) Health(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// CompleteAuthRequest marks an auth request as done after user authentication.
func (s *Storage) CompleteAuthRequest(ctx context.Context, id, userID, email string) error {
	return s.q().CompleteAuthRequest(ctx, dbgen.CompleteAuthRequestParams{
		UserID:  userID,
		UserEmail: email,
		ID:      id,
	})
}

func (s *Storage) lookupEmail(ctx context.Context, userID, clientID string) string {
	row, err := s.q().GetLatestAuthRequestByUserAndClient(ctx, dbgen.GetLatestAuthRequestByUserAndClientParams{
		UserID:   userID,
		ClientID: clientID,
	})
	if err == nil && row.UserEmail != "" {
		return row.UserEmail
	}
	if strings.Contains(userID, "@") {
		return userID
	}
	return ""
}

func (s *Storage) setUserinfo(userinfo *oidc.UserInfo, userID, email string, scopes []string) error {
	userinfo.Subject = userID

	for _, scope := range scopes {
		switch scope {
		case oidc.ScopeEmail:
			userinfo.Email = email
			userinfo.EmailVerified = oidc.Bool(true)
		case oidc.ScopeProfile:
			if email != "" {
				parts := strings.SplitN(email, "@", 2)
				userinfo.PreferredUsername = email
				userinfo.Name = parts[0]
			}
		}
	}
	return nil
}

func clientIDFromRequest(request op.TokenRequest) string {
	if ar, ok := request.(op.AuthRequest); ok {
		return ar.GetClientID()
	}
	return ""
}
