package srv

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
)

const (
	granolaAuthServer    = "https://mcp-auth.granola.ai"
	granolaAuthorizeURL  = granolaAuthServer + "/oauth2/authorize"
	granolaRegisterURL   = granolaAuthServer + "/oauth2/register"
	granolaTokenURL      = granolaAuthServer + "/oauth2/token"
)

type granolaClientRegistration struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

func (s *Server) granolaCallbackURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && !strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return scheme + "://" + r.Host + "/connect/granola/callback"
}

func registerGranolaClient(callbackURL string) (*granolaClientRegistration, error) {
	body, err := json.Marshal(map[string]any{
		"client_name":                "Housecat",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"redirect_uris":              []string{callbackURL},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	if err != nil {
		return nil, errors.Wrap(err, "marshal registration")
	}

	resp, err := http.Post(granolaRegisterURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "register client")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, errors.Newf("registration failed: %d %s", resp.StatusCode, string(respBody))
	}

	var reg granolaClientRegistration
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return nil, errors.Wrap(err, "decode registration")
	}
	return &reg, nil
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", errors.Wrap(err, "generate verifier")
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

func (s *Server) HandleGranolaConnectEnable(c echo.Context) error {
	r := c.Request()
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	callbackURL := s.granolaCallbackURL(r)

	reg, err := registerGranolaClient(callbackURL)
	if err != nil {
		slog.Error("granola register", "error", err)
		return echo.NewHTTPError(http.StatusBadGateway, "failed to register with Granola")
	}

	state, err := randomHex(16)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate state")
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate PKCE")
	}

	stateData, err := json.Marshal(map[string]string{
		"client_id": reg.ClientID,
		"state":     state,
		"verifier":  verifier,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode state")
	}

	c.SetCookie(&http.Cookie{
		Name:     "granola_state",
		Value:    base64.RawURLEncoding.EncodeToString(stateData),
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	params := url.Values{
		"client_id":             {reg.ClientID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {callbackURL},
		"response_type":         {"code"},
		"scope":                 {"openid email offline_access"},
		"state":                 {state},
	}

	return c.Redirect(http.StatusFound, granolaAuthorizeURL+"?"+params.Encode())
}

func (s *Server) HandleGranolaCallback(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"

	stateCookie, err := r.Cookie("granola_state")
	if err != nil || stateCookie.Value == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing state cookie")
	}
	c.SetCookie(&http.Cookie{
		Name:     "granola_state",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	stateJSON, err := base64.RawURLEncoding.DecodeString(stateCookie.Value)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid state cookie")
	}

	var saved struct {
		ClientID string `json:"client_id"`
		State    string `json:"state"`
		Verifier string `json:"verifier"`
	}
	if err := json.Unmarshal(stateJSON, &saved); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid state data")
	}

	if c.QueryParam("state") != saved.State {
		return echo.NewHTTPError(http.StatusBadRequest, "state mismatch")
	}

	if errParam := c.QueryParam("error"); errParam != "" {
		slog.Warn("granola oauth denied", "error", errParam, "description", c.QueryParam("error_description"))
		return c.Redirect(http.StatusFound, "/connect/granola")
	}

	callbackURL := s.granolaCallbackURL(r)

	data := url.Values{
		"client_id":     {saved.ClientID},
		"code":          {c.QueryParam("code")},
		"code_verifier": {saved.Verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {callbackURL},
	}

	resp, err := http.PostForm(granolaTokenURL, data)
	if err != nil {
		slog.Error("granola token exchange", "error", err)
		return echo.NewHTTPError(http.StatusBadGateway, "failed to exchange code")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("granola token exchange", "status", resp.StatusCode, "body", string(respBody))
		return echo.NewHTTPError(http.StatusBadGateway, "token exchange failed")
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return errors.Wrap(err, "decode token response")
	}

	var expiry time.Time
	if tokenResp.ExpiresIn > 0 {
		expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	q := dbgen.New(s.DB)
	if err := q.UpsertOAuthToken(ctx, dbgen.UpsertOAuthTokenParams{
		AccessToken:  tokenResp.AccessToken,
		ExpiresAt:    &expiry,
		Level:        "read",
		Provider:     "granola",
		RefreshToken: tokenResp.RefreshToken,
		Scopes:       "openid,email,offline_access",
		Service:      "granola",
		UserID:       userID,
	}); err != nil {
		return errors.Wrap(err, "save granola token")
	}

	meta := userEmail + " connected Granola (read)"
	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "connected_integration",
		ObjectID:   "granola",
		ObjectType: "integration",
		Metadata:   &meta,
	})

	slog.Info("granola connected", "user", userEmail)
	return c.Redirect(http.StatusFound, "/connect/granola")
}
