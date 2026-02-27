package srv

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestServerSetupAndHandlers(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test_server.sqlite3")
	t.Cleanup(func() { os.Remove(tempDB) })

	server, err := New(tempDB, "test-hostname")
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	e := echo.New()

	t.Run("root endpoint unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		if err := server.HandleRoot(c); err != nil {
			t.Fatalf("HandleRoot error: %v", err)
		}

		body := w.Body.String()
		if !strings.Contains(body, "test-hostname") {
			t.Errorf("expected page to show hostname")
		}
		if !strings.Contains(body, "Go Template Project") {
			t.Errorf("expected page to contain headline")
		}
		if strings.Contains(body, "Signed in as") {
			t.Errorf("expected page to not be logged in")
		}
		if !strings.Contains(body, "Not signed in") {
			t.Errorf("expected page to show 'Not signed in'")
		}
	})

	t.Run("root endpoint authenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-ExeDev-UserID", "user123")
		req.Header.Set("X-ExeDev-Email", "test@example.com")
		w := httptest.NewRecorder()
		c := e.NewContext(req, w)

		if err := server.HandleRoot(c); err != nil {
			t.Fatalf("HandleRoot error: %v", err)
		}

		body := w.Body.String()
		if !strings.Contains(body, "Signed in as") {
			t.Errorf("expected page to show logged in state")
		}
		if !strings.Contains(body, "test@example.com") {
			t.Error("expected page to show user email")
		}
	})

	t.Run("activity count increments", func(t *testing.T) {
		req1 := httptest.NewRequest(http.MethodGet, "/", nil)
		req1.Header.Set("X-ExeDev-UserID", "counter-test")
		w1 := httptest.NewRecorder()
		c1 := e.NewContext(req1, w1)
		if err := server.HandleRoot(c1); err != nil {
			t.Fatalf("HandleRoot error: %v", err)
		}

		body1 := w1.Body.String()
		if !strings.Contains(body1, ">1<") {
			t.Errorf("expected first visit to show 1 activity, got: %s", body1)
		}

		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.Header.Set("X-ExeDev-UserID", "counter-test")
		w2 := httptest.NewRecorder()
		c2 := e.NewContext(req2, w2)
		if err := server.HandleRoot(c2); err != nil {
			t.Fatalf("HandleRoot error: %v", err)
		}

		body2 := w2.Body.String()
		if !strings.Contains(body2, ">2<") {
			t.Errorf("expected second visit to show 2 activities, got: %s", body2)
		}
	})
}

func TestUtilityFunctions(t *testing.T) {
	t.Run("mainDomainFromHost function", func(t *testing.T) {
		tests := []struct {
			input    string
			expected string
		}{
			{"example.exe.cloud:8080", "exe.cloud:8080"},
			{"example.exe.dev", "exe.dev"},
			{"example.exe.cloud", "exe.cloud"},
		}

		for _, test := range tests {
			result := mainDomainFromHost(test.input)
			if result != test.expected {
				t.Errorf("mainDomainFromHost(%q) = %q, expected %q", test.input, result, test.expected)
			}
		}
	})
}
