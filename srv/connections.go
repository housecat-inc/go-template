package srv

import (
	"context"

	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/mcp"
	"github.com/housecat-inc/auth/ui/pages"
)

var servicesByName map[string]mcp.ServiceStatus

func init() {
	servicesByName = make(map[string]mcp.ServiceStatus, len(mcp.Services))
	for _, svc := range mcp.Services {
		servicesByName[svc.Name] = svc
	}
}

func buildIntegrations(services []mcp.ServiceStatus, connectedLevels map[string]map[string]bool) []pages.Integration {
	var out []pages.Integration
	for _, svc := range services {
		var levels []pages.IntegrationLevel
		anyConnected := false
		for _, lvl := range svc.Levels {
			connected := false
			if m, ok := connectedLevels[svc.Name]; ok {
				connected = m[lvl.Level]
			}
			if connected {
				anyConnected = true
			}
			levels = append(levels, pages.IntegrationLevel{
				Connected:   connected,
				DisplayName: lvl.DisplayName,
				Level:       lvl.Level,
			})
		}
		out = append(out, pages.Integration{
			Connected:   anyConnected,
			Description: svc.Description,
			DisplayName: svc.DisplayName,
			Levels:      levels,
			Name:        svc.Name,
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

func buildIntegration(svc mcp.ServiceStatus, connectedLevels map[string]bool) pages.Integration {
	all := buildIntegrations([]mcp.ServiceStatus{svc}, map[string]map[string]bool{svc.Name: connectedLevels})
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
	svc, ok := servicesByName[name]
	if !ok {
		return echo.NewHTTPError(404, "unknown service")
	}

	connectedLevels := s.connectedLevelsForUser(ctx, userID)

	data := pages.ConnectData{
		Integration: buildIntegration(svc, connectedLevels[name]),
		LogoutURL:   logoutURL,
		UserEmail:   userEmail,
	}
	return pages.Connect(data, isAdminWithProvider(userEmail, provider)).Render(ctx, c.Response())
}
