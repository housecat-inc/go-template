package srv

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssuerURL(t *testing.T) {
	server := testServer(t)

	t.Run("localhost returns http", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "localhost:8000"
		a.Equal("http://localhost:8000", server.issuerURL(req))
	})

	t.Run("non-localhost returns https", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "auth.example.com"
		a.Equal("https://auth.example.com", server.issuerURL(req))
	})

	t.Run("respects X-Forwarded-Proto", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "auth.example.com"
		req.Header.Set("X-Forwarded-Proto", "https")
		a.Equal("https://auth.example.com", server.issuerURL(req))
	})

	t.Run("respects X-Forwarded-Host", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "internal:8000"
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "auth.example.com")
		a.Equal("https://auth.example.com", server.issuerURL(req))
	})

	t.Run("handles comma-separated forwarded values", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = "internal:8000"
		req.Header.Set("X-Forwarded-Proto", "https, http")
		req.Header.Set("X-Forwarded-Host", "auth.example.com, proxy.internal")
		a.Equal("https://auth.example.com", server.issuerURL(req))
	})
}

func TestOIDCDiscovery(t *testing.T) {
	server := testServer(t)
	e := echo.New()

	t.Run("well-known endpoint responds", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
		req.Host = "localhost:8000"
		w := httptest.NewRecorder()

		handler := echo.WrapHandler(server.oidcOP)
		c := e.NewContext(req, w)
		c.SetPath("/.well-known/*")

		r := require.New(t)
		r.NoError(handler(c))
		a.Equal(http.StatusOK, w.Code)
		a.Contains(w.Body.String(), "issuer")
	})

	t.Run("JWKS endpoint responds", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/keys", nil)
		req.Host = "localhost:8000"
		w := httptest.NewRecorder()

		handler := echo.WrapHandler(server.oidcOP)
		c := e.NewContext(req, w)
		c.SetPath("/keys")

		r := require.New(t)
		r.NoError(handler(c))
		a.Equal(http.StatusOK, w.Code)
		a.Contains(w.Body.String(), "keys")
	})
}

func TestBuildVMPrompt(t *testing.T) {
	a := assert.New(t)

	prompt := buildVMPrompt("https://auth.example.com", "my-token")
	a.Contains(prompt, "https://my-token@auth.example.com/register")
	a.Contains(prompt, "go run ./cmd/register")
	a.Contains(prompt, "clone -b main")

	prompt = buildVMPrompt("http://localhost:8000", "tok")
	a.Contains(prompt, "http://tok@localhost:8000/register")
}
