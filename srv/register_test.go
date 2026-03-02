package srv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	tempDB := filepath.Join(t.TempDir(), "test.sqlite3")
	t.Cleanup(func() { os.Remove(tempDB) })
	server, err := New(tempDB, "test-hostname", OAuthConfig{SessionSecret: "test-secret"}, "")
	require.New(t).NoError(err)
	return server
}

func TestHandleRegistrationToken(t *testing.T) {
	server := testServer(t)
	e := echo.New()

	t.Run("generates token", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodPost, "/admin/clients/registration-token", nil)
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)
		c.Set("userID", "admin")

		r := require.New(t)
		r.NoError(server.HandleRegistrationToken(c))
		a.Equal(http.StatusOK, w.Code)

		var resp map[string]any
		r.NoError(json.Unmarshal(w.Body.Bytes(), &resp))
		a.NotEmpty(resp["token"])
		a.NotEmpty(resp["expires_at"])
	})
}

func TestHandleRegister(t *testing.T) {
	server := testServer(t)
	e := echo.New()

	t.Run("rejects missing bearer token", func(t *testing.T) {
		a := assert.New(t)
		body := `{"client_name": "test", "redirect_uris": ["https://example.com/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		r := require.New(t)
		r.NoError(server.HandleRegister(c))
		a.Equal(http.StatusUnauthorized, w.Code)
	})

	t.Run("rejects invalid token", func(t *testing.T) {
		a := assert.New(t)
		body := `{"client_name": "test", "redirect_uris": ["https://example.com/callback"]}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer invalid-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		r := require.New(t)
		r.NoError(server.HandleRegister(c))
		a.Equal(http.StatusUnauthorized, w.Code)
	})

	t.Run("successful registration", func(t *testing.T) {
		a := assert.New(t)
		r := require.New(t)

		// Create token
		tokenReq := httptest.NewRequest(http.MethodPost, "/admin/clients/registration-token", nil)
		tokenW := httptest.NewRecorder()
		tokenC := e.NewContext(tokenReq, tokenW)
		tokenC.Set("userID", "admin")
		r.NoError(server.HandleRegistrationToken(tokenC))

		var tokenResp map[string]any
		r.NoError(json.Unmarshal(tokenW.Body.Bytes(), &tokenResp))
		token := tokenResp["token"].(string)

		// Register client
		body := `{"client_name": "my-app", "redirect_uris": ["https://example.com/callback"], "scope": "openid email"}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		r.NoError(server.HandleRegister(c))
		a.Equal(http.StatusCreated, w.Code)

		var resp ClientRegistrationResponse
		r.NoError(json.Unmarshal(w.Body.Bytes(), &resp))
		a.Equal("my-app", resp.ClientName)
		a.NotEmpty(resp.ClientID)
		a.NotEmpty(resp.ClientSecret)
	})

	t.Run("token is single-use", func(t *testing.T) {
		a := assert.New(t)
		r := require.New(t)

		// Create token
		tokenReq := httptest.NewRequest(http.MethodPost, "/admin/clients/registration-token", nil)
		tokenW := httptest.NewRecorder()
		tokenC := e.NewContext(tokenReq, tokenW)
		tokenC.Set("userID", "admin")
		r.NoError(server.HandleRegistrationToken(tokenC))

		var tokenResp map[string]any
		r.NoError(json.Unmarshal(tokenW.Body.Bytes(), &tokenResp))
		token := tokenResp["token"].(string)

		// First use — succeeds
		body := `{"client_name": "app1", "redirect_uris": ["https://example.com/cb"]}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)
		r.NoError(server.HandleRegister(c))
		a.Equal(http.StatusCreated, w.Code)

		// Second use — fails
		body2 := `{"client_name": "app2", "redirect_uris": ["https://example.com/cb"]}`
		req2 := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body2))
		req2.Header.Set("Authorization", "Bearer "+token)
		req2.Header.Set("Content-Type", "application/json")
		w2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, w2)
		r.NoError(server.HandleRegister(c2))
		a.Equal(http.StatusUnauthorized, w2.Code)
	})

	t.Run("rejects invalid scopes", func(t *testing.T) {
		a := assert.New(t)
		r := require.New(t)

		tokenReq := httptest.NewRequest(http.MethodPost, "/admin/clients/registration-token", nil)
		tokenW := httptest.NewRecorder()
		tokenC := e.NewContext(tokenReq, tokenW)
		tokenC.Set("userID", "admin")
		r.NoError(server.HandleRegistrationToken(tokenC))

		var tokenResp map[string]any
		r.NoError(json.Unmarshal(tokenW.Body.Bytes(), &tokenResp))
		token := tokenResp["token"].(string)

		body := `{"client_name": "bad", "redirect_uris": ["https://example.com/cb"], "scope": "openid admin"}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)
		r.NoError(server.HandleRegister(c))
		a.Equal(http.StatusBadRequest, w.Code)
	})

	t.Run("rejects invalid redirect URI", func(t *testing.T) {
		a := assert.New(t)
		r := require.New(t)

		tokenReq := httptest.NewRequest(http.MethodPost, "/admin/clients/registration-token", nil)
		tokenW := httptest.NewRecorder()
		tokenC := e.NewContext(tokenReq, tokenW)
		tokenC.Set("userID", "admin")
		r.NoError(server.HandleRegistrationToken(tokenC))

		var tokenResp map[string]any
		r.NoError(json.Unmarshal(tokenW.Body.Bytes(), &tokenResp))
		token := tokenResp["token"].(string)

		body := `{"client_name": "bad", "redirect_uris": ["not-a-url"]}`
		req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)
		r.NoError(server.HandleRegister(c))
		a.Equal(http.StatusBadRequest, w.Code)
	})
}

func TestHasScope(t *testing.T) {
	a := assert.New(t)
	a.True(hasScope("client:register", "client:register"))
	a.True(hasScope("openid,email,profile", "email"))
	a.True(hasScope("openid, email, profile", "email"))
	a.False(hasScope("openid,email", "admin"))
	a.False(hasScope("", "anything"))
}

func TestValidateScopes(t *testing.T) {
	a := assert.New(t)

	s, err := validateScopes([]string{"openid", "email"})
	a.NoError(err)
	a.Equal("openid,email", s)

	_, err = validateScopes([]string{"openid", "admin"})
	a.Error(err)

	s, err = validateScopes([]string{})
	a.NoError(err)
	a.Equal("", s)

	s, err = validateScopes([]string{" openid ", "profile"})
	a.NoError(err)
	a.Equal("openid,profile", s)
}
