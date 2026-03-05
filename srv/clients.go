package srv

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/ui/pages"
)

var availableScopes = []string{"email", "git", "offline_access", "openid", "profile"}

func (s *Server) HandleClients(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

	q := dbgen.New(s.DB)
	clients, err := q.ListOidcClients(ctx)
	if err != nil {
		return errors.Wrap(err, "list clients")
	}

	count, _ := q.CountOidcClients(ctx)

	data := pages.ClientsListData{
		ClientCount:  count,
		Clients:      clients,
		LogoutURL: logoutURL,
		UserEmail: userEmail,
	}
	return pages.ClientsList(data).Render(ctx, c.Response())
}

func (s *Server) HandleClientsNew(c echo.Context) error {
	r := c.Request()
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

	data := pages.ClientsFormData{
		AvailableScopes: availableScopes,
		LogoutURL:       logoutURL,
		UserEmail:       userEmail,
	}
	return pages.ClientsNew(data).Render(r.Context(), c.Response())
}

func (s *Server) HandleClientsCreate(c echo.Context) error {
	ctx := c.Request().Context()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)

	name := strings.TrimSpace(c.FormValue("name"))
	redirectURIs := joinLines(c.FormValue("redirect_uris"))
	scopes, err := validateScopes(c.Request().Form["scopes"])
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if name == "" || redirectURIs == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and at least one redirect URI are required")
	}

	clientID, err := randomHex(16)
	if err != nil {
		return errors.Wrap(err, "generate client id")
	}
	clientSecret, err := randomHex(32)
	if err != nil {
		return errors.Wrap(err, "generate client secret")
	}

	q := dbgen.New(s.DB)
	client, err := q.InsertOidcClient(ctx, dbgen.InsertOidcClientParams{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Name:         name,
		RedirectUris: redirectURIs,
		Scopes:       scopes,
		CreatedBy:    userID,
	})
	if err != nil {
		return errors.Wrap(err, "insert client")
	}

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "created_client",
		ObjectID:   fmt.Sprintf("%d", client.ID),
		ObjectType: "client",
		Metadata:   &userEmail,
	})

	return c.Redirect(http.StatusFound, fmt.Sprintf("/admin/clients/%d", client.ID))
}

func (s *Server) HandleClientsView(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}

	q := dbgen.New(s.DB)
	client, err := q.GetOidcClient(ctx, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "client not found")
	}

	activities, _ := q.ListActivitiesByObject(ctx, dbgen.ListActivitiesByObjectParams{
		ObjectType: "client",
		ObjectID:   fmt.Sprintf("%d", client.ID),
		Limit:      10,
	})

	var activityEntries []pages.ActivityEntry
	for _, a := range activities {
		meta := ""
		if a.Metadata != nil {
			meta = *a.Metadata
		}
		activityEntries = append(activityEntries, pages.ActivityEntry{
			Action:    a.Action,
			CreatedAt: a.CreatedAt,
			Metadata:  meta,
		})
	}

	data := pages.ClientsViewData{
		Activities: activityEntries,
		Client:     client,
		LogoutURL:  logoutURL,
		UserEmail:  userEmail,
	}
	return pages.ClientsView(data).Render(ctx, c.Response())
}

func (s *Server) HandleClientsEdit(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}

	q := dbgen.New(s.DB)
	client, err := q.GetOidcClient(ctx, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "client not found")
	}

	selectedScopes := make(map[string]bool)
	for _, s := range splitComma(client.Scopes) {
		selectedScopes[s] = true
	}

	data := pages.ClientsFormData{
		Client:          &client,
		AvailableScopes: availableScopes,
		LogoutURL:       logoutURL,
		SelectedScopes:  selectedScopes,
		UserEmail:       userEmail,
	}
	return pages.ClientsEdit(data).Render(ctx, c.Response())
}

func (s *Server) HandleClientsUpdate(c echo.Context) error {
	ctx := c.Request().Context()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}

	name := strings.TrimSpace(c.FormValue("name"))
	redirectURIs := joinLines(c.FormValue("redirect_uris"))
	scopes, scopeErr := validateScopes(c.Request().Form["scopes"])
	if scopeErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, scopeErr.Error())
	}

	if name == "" || redirectURIs == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name and at least one redirect URI are required")
	}

	q := dbgen.New(s.DB)
	if err := q.UpdateOidcClient(ctx, dbgen.UpdateOidcClientParams{
		ID:           id,
		Name:         name,
		RedirectUris: redirectURIs,
		Scopes:       scopes,
	}); err != nil {
		return errors.Wrap(err, "update client")
	}

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "updated_client",
		ObjectID:   fmt.Sprintf("%d", id),
		ObjectType: "client",
		Metadata:   &userEmail,
	})

	return c.Redirect(http.StatusFound, fmt.Sprintf("/admin/clients/%d", id))
}

func (s *Server) HandleClientsArchive(c echo.Context) error {
	ctx := c.Request().Context()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}

	q := dbgen.New(s.DB)
	if err := q.ArchiveOidcClient(ctx, id); err != nil {
		return errors.Wrap(err, "archive client")
	}

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "archived_client",
		ObjectID:   fmt.Sprintf("%d", id),
		ObjectType: "client",
		Metadata:   &userEmail,
	})

	return c.Redirect(http.StatusFound, "/admin/clients")
}

func validateScopes(submitted []string) (string, error) {
	allowed := make(map[string]bool, len(availableScopes))
	for _, s := range availableScopes {
		allowed[s] = true
	}
	var valid []string
	for _, s := range submitted {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !allowed[s] {
			return "", stderrors.New("invalid scope: " + s)
		}
		valid = append(valid, s)
	}
	return strings.Join(valid, ","), nil
}

func joinLines(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, ",")
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
