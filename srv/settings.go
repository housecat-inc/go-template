package srv

import (
	"encoding/json"

	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/ui/pages"
)

type Settings struct {
	BrandingFooter bool `json:"branding_footer"`
}

var DefaultSettings = Settings{
	BrandingFooter: true,
}

func parseSettings(raw string) Settings {
	s := DefaultSettings
	_ = json.Unmarshal([]byte(raw), &s)
	return s
}

func (s *Server) getSettings(c echo.Context) Settings {
	if s.DB == nil {
		return DefaultSettings
	}
	ctx := c.Request().Context()
	subject := c.Get("subject").(string)
	q := dbgen.New(s.DB)
	row, err := q.GetUserSettings(ctx, subject)
	if err != nil {
		return DefaultSettings
	}
	return parseSettings(row.Settings)
}

func (s *Server) HandleSettings(c echo.Context) error {
	ctx := c.Request().Context()
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)
	provider := c.Get("provider").(string)

	settings := s.getSettings(c)

	data := pages.SettingsData{
		BrandingFooter: settings.BrandingFooter,
		LogoutURL:      logoutURL,
		UserEmail:      userEmail,
	}
	return pages.Settings(data, isAdminWithProvider(userEmail, provider)).Render(ctx, c.Response())
}

func (s *Server) HandleSettingsUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	subject := c.Get("subject").(string)

	settings := s.getSettings(c)
	settings.BrandingFooter = c.FormValue("branding_footer") == "on"

	raw, _ := json.Marshal(settings)

	if s.DB != nil {
		q := dbgen.New(s.DB)
		_ = q.UpsertUserSettings(ctx, dbgen.UpsertUserSettingsParams{
			Settings: string(raw),
			Subject:  subject,
		})
	}

	return c.Redirect(303, "/settings")
}
