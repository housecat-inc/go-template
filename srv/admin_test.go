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

func TestRequireAdmin(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_admin.sqlite3")
	t.Cleanup(func() { os.Remove(tempDB) })

	server, err := New(tempDB, "test-hostname", nil, OAuthConfig{}, "")
	r := require.New(t)
	r.NoError(err)

	e := echo.New()

	t.Run("blocks non-housecat email", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/admin/vms", nil)
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)
		c.Set("userEmail", "test@example.com")

		handler := server.RequireAdmin(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		if err != nil {
			he, ok := err.(*echo.HTTPError)
			a.True(ok)
			a.Equal(http.StatusForbidden, he.Code)
		}
	})

	t.Run("allows housecat email", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/admin/vms", nil)
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)
		c.Set("userEmail", "admin@housecat.com")

		handler := server.RequireAdmin(func(c echo.Context) error {
			return c.String(http.StatusOK, "ok")
		})
		err := handler(c)
		a.NoError(err)
		a.Equal(http.StatusOK, w.Code)
	})

	t.Run("admin vms page renders without exedev client", func(t *testing.T) {
		a := assert.New(t)
		req := httptest.NewRequest(http.MethodGet, "/admin/vms", nil)
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)
		c.Set("subject", "admin-user")
		c.Set("userEmail", "admin@housecat.com")
		c.Set("logoutURL", "/auth/logout")
		c.Set("provider", "Google")

		r := require.New(t)
		r.NoError(server.HandleAdminVMs(c))

		body := w.Body.String()
		a.Equal(http.StatusOK, w.Code)
		a.Contains(body, "Virtual Machines")
		a.Contains(body, "EXEDEV_KEY_PATH")
	})
}

func TestIsAdmin(t *testing.T) {
	a := assert.New(t)
	a.True(isAdmin("admin@housecat.com"))
	a.False(isAdmin("user@example.com"))
	a.False(isAdmin(""))
	a.False(isAdmin("housecat.com"))
	a.False(isAdmin("tool@localhost"))
}

func TestIsAdminWithProvider(t *testing.T) {
	a := assert.New(t)
	a.True(isAdminWithProvider("admin@housecat.com", "Google"))
	a.True(isAdminWithProvider("admin@housecat.com", "exe.dev"))
	a.True(isAdminWithProvider("tool@localhost", "localhost"))
	a.False(isAdminWithProvider("tool@localhost", "exe.dev"))
	a.False(isAdminWithProvider("tool@localhost", "Google"))
	a.False(isAdminWithProvider("user@example.com", "localhost"))
}
