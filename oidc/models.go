package oidc

import (
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/housecat-inc/auth/db/dbgen"
)

// AuthRequest implements op.AuthRequest backed by dbgen.OidcAuthRequest.
type AuthRequest struct {
	dbgen.OidcAuthRequest
}

func (a *AuthRequest) GetID() string            { return a.ID }
func (a *AuthRequest) GetACR() string            { return "" }
func (a *AuthRequest) GetAMR() []string          { return nil }
func (a *AuthRequest) GetAudience() []string     { return []string{a.ClientID} }
func (a *AuthRequest) GetClientID() string       { return a.ClientID }
func (a *AuthRequest) GetLoginHint() string      { return a.LoginHint }
func (a *AuthRequest) GetNonce() string           { return a.Nonce }
func (a *AuthRequest) GetRedirectURI() string    { return a.RedirectUri }
func (a *AuthRequest) GetResponseMode() oidc.ResponseMode { return "" }
func (a *AuthRequest) GetScopes() []string       { return splitComma(a.Scopes) }
func (a *AuthRequest) GetState() string           { return a.State }
func (a *AuthRequest) GetSubject() string         { return a.Subject }
func (a *AuthRequest) Done() bool                 { return a.OidcAuthRequest.Done != 0 }

func (a *AuthRequest) GetAuthTime() time.Time {
	if a.AuthTime != nil {
		return *a.AuthTime
	}
	return time.Time{}
}

func (a *AuthRequest) GetResponseType() oidc.ResponseType {
	return oidc.ResponseType(a.ResponseType)
}

func (a *AuthRequest) GetCodeChallenge() *oidc.CodeChallenge {
	if a.CodeChallenge == "" {
		return nil
	}
	return &oidc.CodeChallenge{
		Challenge: a.CodeChallenge,
		Method:    oidc.CodeChallengeMethod(a.CodeChallengeMethod),
	}
}

// Client implements op.Client backed by dbgen.OidcClient.
type Client struct {
	dbgen.OidcClient
	loginURL string
}

func NewClient(c dbgen.OidcClient, loginURL string) *Client {
	return &Client{OidcClient: c, loginURL: loginURL}
}

func (c *Client) GetID() string                             { return c.ClientID }
func (c *Client) RedirectURIs() []string                    { return splitComma(c.RedirectUris) }
func (c *Client) PostLogoutRedirectURIs() []string          { return splitComma(c.PostLogoutRedirectUris) }
func (c *Client) LoginURL(id string) string                 { return c.loginURL + "?authRequestID=" + id }
func (c *Client) IDTokenLifetime() time.Duration            { return 1 * time.Hour }
func (c *Client) DevMode() bool                             { return false }
func (c *Client) IDTokenUserinfoClaimsAssertion() bool      { return true }
func (c *Client) ClockSkew() time.Duration                  { return 0 }
func (c *Client) IsScopeAllowed(scope string) bool {
	if scope == "" {
		return false
	}
	for _, allowed := range splitComma(c.Scopes) {
		if strings.TrimSpace(allowed) == scope {
			return true
		}
	}
	return false
}

func (c *Client) RestrictAdditionalIdTokenScopes() func(scopes []string) []string {
	return func(scopes []string) []string { return scopes }
}

func (c *Client) RestrictAdditionalAccessTokenScopes() func(scopes []string) []string {
	return func(scopes []string) []string { return scopes }
}

func (c *Client) ApplicationType() op.ApplicationType {
	switch c.OidcClient.ApplicationType {
	case "native":
		return op.ApplicationTypeNative
	case "user_agent":
		return op.ApplicationTypeUserAgent
	default:
		return op.ApplicationTypeWeb
	}
}

func (c *Client) AuthMethod() oidc.AuthMethod {
	switch c.OidcClient.AuthMethod {
	case "client_secret_post":
		return oidc.AuthMethodPost
	case "none":
		return oidc.AuthMethodNone
	default:
		return oidc.AuthMethodBasic
	}
}

func (c *Client) ResponseTypes() []oidc.ResponseType {
	var out []oidc.ResponseType
	for _, rt := range splitComma(c.OidcClient.ResponseTypes) {
		out = append(out, oidc.ResponseType(rt))
	}
	return out
}

func (c *Client) GrantTypes() []oidc.GrantType {
	var out []oidc.GrantType
	for _, gt := range splitComma(c.OidcClient.GrantTypes) {
		out = append(out, oidc.GrantType(gt))
	}
	return out
}

func (c *Client) AccessTokenType() op.AccessTokenType {
	if c.OidcClient.AccessTokenType == "JWT" {
		return op.AccessTokenTypeJWT
	}
	return op.AccessTokenTypeBearer
}

// RefreshToken implements op.RefreshTokenRequest backed by dbgen.OidcRefreshToken.
type RefreshToken struct {
	dbgen.OidcRefreshToken
	currentScopes []string
}

func (r *RefreshToken) GetAMR() []string        { return nil }
func (r *RefreshToken) GetAudience() []string    { return splitComma(r.Audience) }
func (r *RefreshToken) GetAuthTime() time.Time   { return r.AuthTime }
func (r *RefreshToken) GetClientID() string      { return r.ApplicationID }
func (r *RefreshToken) GetScopes() []string {
	if r.currentScopes != nil {
		return r.currentScopes
	}
	return splitComma(r.Scopes)
}
func (r *RefreshToken) GetSubject() string       { return r.Subject }
func (r *RefreshToken) SetCurrentScopes(scopes []string) { r.currentScopes = scopes }

// signingKey implements op.SigningKey.
type signingKey struct {
	id  string
	alg jose.SignatureAlgorithm
	key any
}

func (s *signingKey) SignatureAlgorithm() jose.SignatureAlgorithm { return s.alg }
func (s *signingKey) Key() any                                    { return s.key }
func (s *signingKey) ID() string                                  { return s.id }

// publicKey implements op.Key.
type publicKey struct {
	id  string
	alg jose.SignatureAlgorithm
	key any
}

func (p *publicKey) ID() string                          { return p.id }
func (p *publicKey) Algorithm() jose.SignatureAlgorithm  { return p.alg }
func (p *publicKey) Use() string                         { return "sig" }
func (p *publicKey) Key() any                            { return p.key }

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
