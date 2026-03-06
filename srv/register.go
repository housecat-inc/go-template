package srv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
)

// ClientRegistrationRequest is the RFC 7591 registration request body.
type ClientRegistrationRequest struct {
	AllowedDomain           *string  `json:"allowed_domain"`
	AllowedEmails           []string `json:"allowed_emails"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

// ClientRegistrationResponse is the RFC 7591 registration response body.
type ClientRegistrationResponse struct {
	AllowedDomain           string   `json:"allowed_domain"`
	AllowedEmails           []string `json:"allowed_emails"`
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
}

type registrationError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// HandleRegister implements RFC 7591 Dynamic Client Registration.
// It authenticates via a Bearer token (Initial Access Token) that must
// have the "client:register" scope in the oidc_access_tokens table.
func (s *Server) HandleRegister(c echo.Context) error {
	ctx := c.Request().Context()

	authHeader := c.Request().Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return c.JSON(http.StatusUnauthorized, registrationError{
			Error:            "invalid_token",
			ErrorDescription: "Bearer token required",
		})
	}
	tokenID := strings.TrimPrefix(authHeader, "Bearer ")

	q := dbgen.New(s.DB)
	token, err := q.GetAccessToken(ctx, tokenID)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, registrationError{
			Error:            "invalid_token",
			ErrorDescription: "invalid or expired registration token",
		})
	}

	if token.ApplicationID != registrationAppID {
		return c.JSON(http.StatusUnauthorized, registrationError{
			Error:            "invalid_token",
			ErrorDescription: "invalid registration token",
		})
	}

	if !hasScope(token.Scopes, registrationScope) {
		return c.JSON(http.StatusForbidden, registrationError{
			Error:            "insufficient_scope",
			ErrorDescription: "token does not have client:register scope",
		})
	}

	var req ClientRegistrationRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return c.JSON(http.StatusBadRequest, registrationError{
			Error:            "invalid_client_metadata",
			ErrorDescription: "invalid JSON body",
		})
	}

	if req.ClientName == "" || len(req.RedirectURIs) == 0 {
		return c.JSON(http.StatusBadRequest, registrationError{
			Error:            "invalid_client_metadata",
			ErrorDescription: "client_name and redirect_uris are required",
		})
	}

	for _, uri := range req.RedirectURIs {
		parsed, err := url.Parse(uri)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return c.JSON(http.StatusBadRequest, registrationError{
				Error:            "invalid_redirect_uri",
				ErrorDescription: "invalid redirect URI: " + uri,
			})
		}
	}

	// RFC 7591 defaults
	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code"}
	}
	if len(req.ResponseTypes) == 0 {
		req.ResponseTypes = []string{"code"}
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "client_secret_basic"
	}
	if req.Scope == "" {
		req.Scope = "openid email profile"
	}

	allowed := map[string]bool{"email": true, "git": true, "offline_access": true, "openid": true, "profile": true}
	hasOfflineAccess := false
	for _, s := range strings.Fields(req.Scope) {
		if !allowed[s] {
			return c.JSON(http.StatusBadRequest, registrationError{
				Error:            "invalid_client_metadata",
				ErrorDescription: "invalid scope: " + s,
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

	internalScopes := strings.ReplaceAll(req.Scope, " ", ",")

	allowedDomain := "housecat.com"
	if req.AllowedDomain != nil {
		allowedDomain = strings.TrimSpace(*req.AllowedDomain)
	}
	allowedEmails := strings.Join(req.AllowedEmails, ",")

	client, err := q.InsertOidcClientFull(ctx, dbgen.InsertOidcClientFullParams{
		AllowedDomain: allowedDomain,
		AllowedEmails: allowedEmails,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		Name:          req.ClientName,
		RedirectUris:  strings.Join(req.RedirectURIs, ","),
		Scopes:        internalScopes,
		AuthMethod:    req.TokenEndpointAuthMethod,
		GrantTypes:    strings.Join(req.GrantTypes, ","),
		CreatedBy:     token.Subject,
	})
	if err != nil {
		return errors.Wrap(err, "insert client")
	}

	// Single-use: delete the registration token
	_ = q.DeleteAccessToken(ctx, tokenID)

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    token.Subject,
		ActorType:  "user",
		Action:     "registered_client",
		ObjectID:   fmt.Sprintf("%d", client.ID),
		ObjectType: "client",
		Metadata:   &req.ClientName,
	})

	var respEmails []string
	if allowedEmails != "" {
		respEmails = strings.Split(allowedEmails, ",")
	}

	return c.JSON(http.StatusCreated, ClientRegistrationResponse{
		AllowedDomain:           allowedDomain,
		AllowedEmails:           respEmails,
		ClientID:                client.ClientID,
		ClientSecret:            client.ClientSecret,
		ClientName:              client.Name,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              req.GrantTypes,
		ResponseTypes:           req.ResponseTypes,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		Scope:                   req.Scope,
		ClientIDIssuedAt:        client.CreatedAt.Unix(),
		ClientSecretExpiresAt:   0,
	})
}

func hasScope(tokenScopes, target string) bool {
	for _, s := range strings.Split(tokenScopes, ",") {
		if strings.TrimSpace(s) == target {
			return true
		}
	}
	return false
}
