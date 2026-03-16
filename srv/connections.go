package srv

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/mcp"
	"github.com/housecat-inc/auth/ui/pages"
)

var servicesByID map[string]mcp.Service

func init() {
	servicesByID = make(map[string]mcp.Service, len(mcp.Services))
	for _, svc := range mcp.Services {
		servicesByID[svc.ID] = svc
	}
}

func buildIntegrations(services []mcp.Service, connectedLevels map[string]map[string]bool) []pages.Integration {
	var out []pages.Integration
	for _, svc := range services {
		var levels []pages.IntegrationLevel
		anyConnected := false
		for _, conn := range svc.Connections {
			connected := false
			if m, ok := connectedLevels[svc.ID]; ok {
				connected = m[conn.Level]
			}
			if connected {
				anyConnected = true
			}
			levels = append(levels, pages.IntegrationLevel{
				Connected:   connected,
				DisplayName: conn.Description,
				Level:       conn.Level,
			})
		}
		out = append(out, pages.Integration{
			Connected:   anyConnected,
			Description: svc.Description,
			DisplayName: svc.Name,
			Levels:      levels,
			Name:        svc.ID,
		})
	}
	return out
}


func (s *Server) connectedLevelsForUser(ctx context.Context, userID string) map[string]map[string]bool {
	out := make(map[string]map[string]bool)
	if s.DB == nil {
		return out
	}
	q := dbgen.New(s.DB)
	tokens, err := q.ListOAuthTokensByUser(ctx, userID)
	if err != nil {
		return out
	}
	for _, t := range tokens {
		if out[t.Service] == nil {
			out[t.Service] = make(map[string]bool)
		}
		out[t.Service][t.Level] = true
	}
	return out
}

func (s *Server) HandleHome(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)
	provider := c.Get("provider").(string)

	connectedLevels := s.connectedLevelsForUser(ctx, userID)
	integrations := buildIntegrations(mcp.Services, connectedLevels)

	var activities []pages.ActivityEntry
	if s.DB != nil {
		q := dbgen.New(s.DB)
		dbActivities, err := q.ListActivitiesByActorAndObjectType(ctx, dbgen.ListActivitiesByActorAndObjectTypeParams{
			ActorID:    userID,
			ObjectType: "integration",
			Limit:      10,
		})
		if err == nil {
			for _, a := range dbActivities {
				meta := ""
				if a.Metadata != nil {
					meta = *a.Metadata
				}
				activities = append(activities, pages.ActivityEntry{
					Action:    a.Action,
					CreatedAt: a.CreatedAt,
					Metadata:  meta,
				})
			}
		}
	}

	data := pages.IntegrationsData{
		Activities:   activities,
		Integrations: integrations,
		LogoutURL:    logoutURL,
		UserEmail:    userEmail,
	}
	return pages.Integrations(data, isAdminWithProvider(userEmail, provider)).Render(ctx, c.Response())
}

func buildIntegration(svc mcp.Service, connectedLevels map[string]bool) pages.Integration {
	all := buildIntegrations([]mcp.Service{svc}, map[string]map[string]bool{svc.ID: connectedLevels})
	return all[0]
}

func (s *Server) HandleConnect(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)
	provider := c.Get("provider").(string)

	name := c.Param("service")
	svc, ok := servicesByID[name]
	if !ok {
		return echo.NewHTTPError(404, "unknown service")
	}

	connectedLevels := s.connectedLevelsForUser(ctx, userID)

	justConnected := c.QueryParam("connected") == "1"
	externalFlow := false
	if justConnected {
		if cookie, err := r.Cookie("connect_external"); err == nil && cookie.Value == "1" {
			externalFlow = true
			// Clear the cookie.
			secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
			c.SetCookie(&http.Cookie{
				Name:     "connect_external",
				Value:    "",
				Path:     "/",
				HttpOnly: true,
				Secure:   secure,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   -1,
			})
		}
	}

	data := pages.ConnectData{
		ExternalFlow:  externalFlow,
		Integration:   buildIntegration(svc, connectedLevels[name]),
		JustConnected: justConnected,
		LogoutURL:     logoutURL,
		UserEmail:     userEmail,
	}
	return pages.Connect(data, isAdminWithProvider(userEmail, provider)).Render(ctx, c.Response())
}
