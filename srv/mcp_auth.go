package srv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/housecat-inc/auth/db/dbgen"
)

func (s *Server) verifyMCPToken(ctx context.Context, token string, req *http.Request) (*mcpauth.TokenInfo, error) {
	decrypted, err := s.oidcCrypto.Decrypt(token)
	if err != nil {
		slog.Warn("mcp auth: decrypt failed", "error", err)
		return nil, mcpauth.ErrInvalidToken
	}
	tokenID, _, ok := strings.Cut(decrypted, ":")
	if !ok {
		slog.Warn("mcp auth: malformed token")
		return nil, mcpauth.ErrInvalidToken
	}
	q := dbgen.New(s.DB)
	row, err := q.GetAccessToken(ctx, tokenID)
	if err != nil {
		if s.accessTokenExists(ctx, tokenID) {
			slog.Warn("mcp auth: token expired", "tokenID", tokenID)
			return nil, fmt.Errorf("token expired: use refresh_token to obtain a new access token: %w", mcpauth.ErrInvalidToken)
		}
		slog.Warn("mcp auth: token not found", "tokenID", tokenID, "error", err)
		return nil, mcpauth.ErrInvalidToken
	}
	if row.ExpiresAt.Before(time.Now()) {
		slog.Warn("mcp auth: token expired", "tokenID", tokenID, "subject", row.Subject, "expired_at", row.ExpiresAt)
		return nil, fmt.Errorf("token expired: use refresh_token to obtain a new access token: %w", mcpauth.ErrInvalidToken)
	}

	email, _ := q.GetEmailBySubject(ctx, row.Subject)

	extra := map[string]any{"email": email}
	if ref := req.Header.Get("Referer"); ref != "" {
		extra["referer"] = ref
	}
	slog.Info("mcp auth ok", "user", row.Subject, "email", email, "referer", req.Header.Get("Referer"), "origin", req.Header.Get("Origin"), "user_agent", req.Header.Get("User-Agent"))

	scopes := strings.Split(row.Scopes, ",")
	return &mcpauth.TokenInfo{
		Expiration: row.ExpiresAt,
		Extra:      extra,
		Scopes:     scopes,
		UserID:     row.Subject,
	}, nil
}

func (s *Server) accessTokenExists(ctx context.Context, tokenID string) bool {
	var exists bool
	_ = s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM oidc_access_tokens WHERE id = ?)", tokenID).Scan(&exists)
	return exists
}

func (s *Server) handleDiscoveryWithRegistration(oidcHandler http.Handler) echo.HandlerFunc {
	return func(c echo.Context) error {
		rec := &responseRecorder{header: http.Header{}, body: &strings.Builder{}}
		oidcHandler.ServeHTTP(rec, c.Request())

		var discovery map[string]any
		if err := json.Unmarshal([]byte(rec.body.String()), &discovery); err != nil {
			return c.String(rec.code, rec.body.String())
		}

		issuer := "https://" + s.Hostname
		discovery["registration_endpoint"] = issuer + "/register"

		for k, vals := range rec.header {
			for _, v := range vals {
				c.Response().Header().Add(k, v)
			}
		}
		return c.JSON(rec.code, discovery)
	}
}

type responseRecorder struct {
	code   int
	header http.Header
	body   *strings.Builder
}

func (r *responseRecorder) Header() http.Header         { return r.header }
func (r *responseRecorder) WriteHeader(code int)         { r.code = code }
func (r *responseRecorder) Write(b []byte) (int, error)  { return r.body.Write(b) }

type MCPRegistrationRequest struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
	Scope        string   `json:"scope"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
}

type MCPRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
}

func (s *Server) HandleMCPRegister(c echo.Context) error {
	ctx := c.Request().Context()

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
	}

	var req MCPRegistrationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_client_metadata"})
	}

	if req.ClientName == "" {
		req.ClientName = "MCP Client"
	}
	if len(req.RedirectURIs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error":             "invalid_client_metadata",
			"error_description": "redirect_uris required",
		})
	}
	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code"}
	}
	if len(req.ResponseTypes) == 0 {
		req.ResponseTypes = []string{"code"}
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "client_secret_post"
	}
	if req.Scope == "" {
		req.Scope = "openid email profile"
	}

	allowedScopes := map[string]bool{"email": true, "offline_access": true, "openid": true, "profile": true}
	hasOfflineAccess := false
	for _, s := range strings.Fields(req.Scope) {
		if !allowedScopes[s] {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error":             "invalid_client_metadata",
				"error_description": "invalid scope: " + s,
			})
		}
		if s == "offline_access" {
			hasOfflineAccess = true
		}
	}
	if hasOfflineAccess {
		hasRefreshGrant := false
		for _, gt := range req.GrantTypes {
			if gt == "refresh_token" {
				hasRefreshGrant = true
				break
			}
		}
		if !hasRefreshGrant {
			req.GrantTypes = append(req.GrantTypes, "refresh_token")
		}
	}

	clientID, err := randomHex(16)
	if err != nil {
		return errors.Wrap(err, "generate client id")
	}
	clientSecret, err := randomHex(32)
	if err != nil {
		return errors.Wrap(err, "generate client secret")
	}

	q := dbgen.New(s.DB)
	internalScopes := strings.ReplaceAll(req.Scope, " ", ",")
	client, err := q.InsertOidcClientFull(ctx, dbgen.InsertOidcClientFullParams{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Name:         req.ClientName,
		RedirectUris: strings.Join(req.RedirectURIs, ","),
		Scopes:       internalScopes,
		AuthMethod:   req.TokenEndpointAuthMethod,
		GrantTypes:   strings.Join(req.GrantTypes, ","),
		CreatedBy:    "mcp:dynamic",
	})
	if err != nil {
		return errors.Wrap(err, "insert client")
	}

	return c.JSON(http.StatusCreated, MCPRegistrationResponse{
		ClientID:                client.ClientID,
		ClientName:              client.Name,
		ClientSecret:            client.ClientSecret,
		ClientIDIssuedAt:        client.CreatedAt.Unix(),
		ClientSecretExpiresAt:   0,
		GrantTypes:              req.GrantTypes,
		RedirectURIs:            req.RedirectURIs,
		ResponseTypes:           req.ResponseTypes,
		Scope:                   req.Scope,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
	})
}
