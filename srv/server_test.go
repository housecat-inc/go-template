package srv

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerSetupAndHandlers(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_server.sqlite3")
	t.Cleanup(func() { os.Remove(tempDB) })

	server, err := New(tempDB, "test-hostname", OAuthConfig{})
	r := require.New(t)
	r.NoError(err)

	e := echo.New()

	t.Run("root endpoint unauthenticated", func(t *testing.T) {
		a := assert.New(t)
		r := require.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		r.NoError(server.HandleRoot(c))

		body := w.Body.String()
		a.Contains(body, "Sign in")
		a.Contains(body, "exe.dev")
	})

	t.Run("root endpoint authenticated", func(t *testing.T) {
		a := assert.New(t)
		r := require.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-ExeDev-UserID", "user123")
		req.Header.Set("X-ExeDev-Email", "test@example.com")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		r.NoError(server.HandleRoot(c))

		body := w.Body.String()
		a.Contains(body, "Signed in as")
		a.Contains(body, "test@example.com")
	})

	t.Run("activity count increments", func(t *testing.T) {
		a := assert.New(t)
		r := require.New(t)

		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		req1.Header.Set("X-ExeDev-UserID", "counter-test")
		w1 := httptest.NewRecorder()
		c1 := e.NewContext(req1, w1)
		r.NoError(server.HandleRoot(c1))
		a.Contains(w1.Body.String(), ">1<")

		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.Header.Set("X-ExeDev-UserID", "counter-test")
		w2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, w2)
		r.NoError(server.HandleRoot(c2))
		a.Contains(w2.Body.String(), ">2<")
	})
}

func TestUtilityFunctions(t *testing.T) {
	t.Run("mainDomainFromHost", func(t *testing.T) {
		a := assert.New(t)
		a.Equal("exe.cloud:8080", mainDomainFromHost("example.exe.cloud:8080"))
		a.Equal("exe.dev", mainDomainFromHost("example.exe.dev"))
		a.Equal("exe.cloud", mainDomainFromHost("example.exe.cloud"))
	})
}
