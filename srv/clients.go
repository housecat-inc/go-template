package srv

import (
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
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
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)

	allowedDomain := strings.TrimSpace(c.FormValue("allowed_domain"))
	allowedEmails := normalizeEmailList(c.FormValue("allowed_emails"))
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
		AllowedDomain: allowedDomain,
		AllowedEmails: allowedEmails,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		Name:          name,
		RedirectUris:  redirectURIs,
		Scopes:        scopes,
		CreatedBy:     subject,
	})
	if err != nil {
		return errors.Wrap(err, "insert client")
	}

	if err := syncClientAccess(ctx, q, client.ID, allowedDomain, allowedEmails); err != nil {
		return errors.Wrap(err, "sync client access")
	}

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    subject,
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
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}

	allowedDomain := strings.TrimSpace(c.FormValue("allowed_domain"))
	allowedEmails := normalizeEmailList(c.FormValue("allowed_emails"))
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
		AllowedDomain: allowedDomain,
		AllowedEmails: allowedEmails,
		ID:            id,
		Name:          name,
		RedirectUris:  redirectURIs,
		Scopes:        scopes,
	}); err != nil {
		return errors.Wrap(err, "update client")
	}

	if err := syncClientAccess(ctx, q, id, allowedDomain, allowedEmails); err != nil {
		return errors.Wrap(err, "sync client access")
	}

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    subject,
		ActorType:  "user",
		Action:     "updated_client",
		ObjectID:   fmt.Sprintf("%d", id),
		ObjectType: "client",
		Metadata:   &userEmail,
	})

	// If access is restricted to just housecat.com with no extra emails, keep
	// the VM private. Otherwise make it public so external users can reach it.
	if s.ExeDev != nil {
		isDefault := (allowedDomain == "housecat.com" || allowedDomain == "") && allowedEmails == ""
		if isDefault {
			if err := s.ExeDev.ShareSetPrivate(ctx, name); err != nil {
				slog.Warn("exe.dev share set-private failed", "error", err)
			} else {
				slog.Info("exe.dev share set-private", "vm", name)
			}
		} else {
			if err := s.ExeDev.ShareSetPublic(ctx, name); err != nil {
				slog.Warn("exe.dev share set-public failed", "error", err)
			} else {
				slog.Info("exe.dev share set-public", "vm", name)
			}
		}
	}

	return c.Redirect(http.StatusFound, fmt.Sprintf("/admin/clients/%d", id))
}

func (s *Server) HandleClientsArchive(c echo.Context) error {
	ctx := c.Request().Context()
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}

	q := dbgen.New(s.DB)
	if err := q.ArchiveOidcClient(ctx, id); err != nil {
		return errors.Wrap(err, "archive client")
	}

	if err := q.DeleteClientAccessByClientID(ctx, id); err != nil {
		slog.Warn("delete client access on archive", "error", err)
	}

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    subject,
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

func syncClientAccess(ctx context.Context, q *dbgen.Queries, clientID int64, domain, emails string) error {
	if err := q.DeleteClientAccessByClientID(ctx, clientID); err != nil {
		return errors.Wrap(err, "delete client access")
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain != "" {
		if err := q.InsertClientAccess(ctx, dbgen.InsertClientAccessParams{
			ClientID: clientID,
			Domain:   domain,
		}); err != nil {
			return errors.Wrap(err, "insert client access domain")
		}
	}
	if emails != "" {
		for _, email := range strings.Split(emails, ",") {
			email = strings.ToLower(strings.TrimSpace(email))
			if email == "" {
				continue
			}
			if err := q.InsertClientAccess(ctx, dbgen.InsertClientAccessParams{
				ClientID: clientID,
				Email:    email,
			}); err != nil {
				return errors.Wrap(err, "insert client access email")
			}
		}
	}
	return nil
}

func normalizeEmailList(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		for _, email := range strings.Split(line, ",") {
			email = strings.ToLower(strings.TrimSpace(email))
			if email != "" {
				out = append(out, email)
			}
		}
	}
	return strings.Join(out, ",")
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
