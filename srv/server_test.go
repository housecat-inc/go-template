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

	server, err := New(tempDB, "test-hostname", OAuthConfig{}, "")
	r := require.New(t)
	r.NoError(err)

	e := echo.New()

	t.Run("root unauthenticated shows sign-in", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		r := require.New(t)
		r.NoError(server.HandleRoot(c))

		a.Equal(http.StatusOK, w.Code)
		a.Contains(w.Body.String(), "Sign in")
		a.Contains(w.Body.String(), "exe.dev")
	})

	t.Run("root always shows sign-in", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-ExeDev-UserID", "user123")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		r := require.New(t)
		r.NoError(server.HandleRoot(c))

		a.Equal(http.StatusOK, w.Code)
		a.Contains(w.Body.String(), "Sign in")
	})

	t.Run("profile unauthenticated redirects to /", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/profile", nil)
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		handler := server.RequireAuth(server.HandleProfile)
		r := require.New(t)
		r.NoError(handler(c))

		a.Equal(http.StatusFound, w.Code)
		a.Equal("/", w.Header().Get("Location"))
	})

	t.Run("profile authenticated shows welcome page", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/profile", nil)
		req.Header.Set("X-ExeDev-UserID", "user123")
		req.Header.Set("X-ExeDev-Email", "test@example.com")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		handler := server.RequireAuth(server.HandleProfile)
		r := require.New(t)
		r.NoError(handler(c))

		body := w.Body.String()
		a.Equal(http.StatusOK, w.Code)
		a.Contains(body, "Signed in as")
		a.Contains(body, "test@example.com")
	})

	t.Run("activity count increments", func(t *testing.T) {
		a := assert.New(t)
		r := require.New(t)

		req1 := httptest.NewRequest(http.MethodGet, "/profile", nil)
		req1.Header.Set("X-ExeDev-UserID", "counter-test")
		w1 := httptest.NewRecorder()
		c1 := e.NewContext(req1, w1)
		handler := server.RequireAuth(server.HandleProfile)
		r.NoError(handler(c1))
		a.Contains(w1.Body.String(), ">1<")

		req2 := httptest.NewRequest(http.MethodGet, "/profile", nil)
		req2.Header.Set("X-ExeDev-UserID", "counter-test")
		w2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, w2)
		handler2 := server.RequireAuth(server.HandleProfile)
		r.NoError(handler2(c2))
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
