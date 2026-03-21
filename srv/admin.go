package srv

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/exedev"
	"github.com/housecat-inc/auth/ui/pages"
)

func (s *Server) HandleAdminVMs(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

	var vms []exedev.VM
	var vmErr error
	if s.ExeDev != nil {
		vms, vmErr = s.ExeDev.ListVMs(ctx)
		if vmErr != nil {
			slog.Error("list vms", "error", vmErr)
		}
		for i := range vms {
			info, err := s.ExeDev.ShareShow(ctx, vms[i].Name)
			if err != nil {
				slog.Error("share show", "error", err, "vm", vms[i].Name)
				continue
			}
			vms[i].ShareStatus = info.Status
		}
	}

	creators := map[string]pages.VMCreator{}
	var activities []pages.ActivityEntry
	if s.DB != nil {
		q := dbgen.New(s.DB)

		if rows, err := q.ListVMCreators(ctx); err == nil {
			for _, r := range rows {
				email := ""
				if r.Metadata != nil {
					email = *r.Metadata
					if i := strings.Index(email, " ("); i > 0 {
						email = email[:i]
					}
				}
				creators[r.ObjectID] = pages.VMCreator{
					CreatedAt: r.CreatedAt,
					Email:     email,
				}
			}
		}

		_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
			ActorID:    subject,
			ActorType:  "user",
			Action:     "admin_page_view",
			ObjectID:   "vms",
			ObjectType: "page",
			Metadata:   &userEmail,
		})

		dbActivities, err := q.ListActivitiesByObjectType(ctx, dbgen.ListActivitiesByObjectTypeParams{
			ObjectType: "vm",
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

		if len(activities) == 0 {
			dbActivities, err = q.ListActivitiesByActor(ctx, dbgen.ListActivitiesByActorParams{
				ActorID: subject,
				Limit:   10,
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
	}

	clients := map[string]pages.VMClientInfo{}
	if s.DB != nil {
		q := dbgen.New(s.DB)
		if rows, err := q.ListOidcClients(ctx); err == nil {
			for _, c := range rows {
				clients[c.Name] = pages.VMClientInfo{
					AllowedDomain:   c.AllowedDomain,
					AllowedEmails:   c.AllowedEmails,
					HasCustomDomain: strings.Contains(c.RedirectUris, ".vm.housecat.io"),
					ID:              c.ID,
				}
			}
		}
	}

	data := pages.AdminVMsData{
		Activities: activities,
		Clients:    clients,
		Configured: s.ExeDev != nil,
		Creators:   creators,
		Error:      vmErr,
		Hostname:   s.issuerURL(r),
		LogoutURL:  logoutURL,
		UserEmail:  userEmail,
		VMs:        vms,
	}
	component := pages.AdminVMs(data)
	return component.Render(r.Context(), c.Response())
}

// HandleAdminOpenVM redirects to the target VM URL if the user is already
// authenticated via exe.dev. Otherwise it redirects to a magic link so
// they get logged into exe.dev first. The next click will go direct.
func (s *Server) HandleAdminOpenVM(c echo.Context) error {
	ctx := c.Request().Context()
	target := c.QueryParam("url")
	if target == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "url parameter required")
	}
	// Prevent open redirect: only allow *.exe.xyz targets.
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "https" || !strings.HasSuffix(parsed.Hostname(), ".exe.xyz") {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid target URL")
	}

	if s.ExeDev == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "exe.dev client not configured")
	}

	magicLink, err := s.ExeDev.BrowserLink(ctx)
	if err != nil {
		slog.Error("browser link", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate login link")
	}

	escapedMagicLink := html.EscapeString(magicLink)
	escapedTarget := html.EscapeString(target)
	return c.HTML(http.StatusOK, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Opening…</title></head>
<body><p>Signing in…</p>
<script>
var w=window.open("`+escapedMagicLink+`","_blank");
setTimeout(function(){if(w)try{w.close()}catch(e){}window.location.href="`+escapedTarget+`"},750);
</script>
</body></html>`)
}

func (s *Server) HandleAdminNewVM(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)

	if s.ExeDev == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "exe.dev client not configured")
	}

	app := c.FormValue("app")
	if app == "" {
		app = "go-template"
	}
	if app != "go-template" && app != "app" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid app")
	}

	token, _, err := s.createRegistrationToken(ctx, subject)
	if err != nil {
		return err
	}

	q := dbgen.New(s.DB)

	vmName := GenerateName()
	slog.Info("creating vm", "name", vmName, "app", app, "user", userEmail)

	result, err := s.ExeDev.CreateVM(ctx, vmName)
	if err != nil {
		return errors.Wrap(err, "create vm")
	}

	slog.Info("vm created", "name", result.Name, "app", app, "user", userEmail)

	if s.DNS != nil {
		if err := s.DNS.CreateCNAME(ctx, result.Name); err != nil {
			slog.Error("dns create cname", "vm", result.Name, "error", err)
		}
	}

	meta := userEmail + " (" + app + ")"
	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    subject,
		ActorType:  "user",
		Action:     "created_vm",
		ObjectID:   result.Name,
		ObjectType: "vm",
		Metadata:   &meta,
	})

	issue := s.issuerURL(r)
	repo := "housecat-inc/" + app
	registerRef := "main"
	if s.GitProxy != nil {
		if sha, err := s.GitProxy.ResolveBranch(ctx, "housecat-inc/go-template", "main"); err == nil {
			registerRef = sha
		}
	}
	prompt := buildVMPrompt(issue, token, repo, registerRef)

	s.vmSetups.Store(result.Name, &vmSetup{ShelleyURL: result.ShelleyURL})

	go func() {
		for attempt := range 60 {
			time.Sleep(5 * time.Second)
			res, err := s.ExeDev.SendPrompt(context.Background(), result.Name, prompt, "claude-sonnet-4.5")
			if err != nil {
				slog.Warn("send prompt retry", "vm", result.Name, "attempt", attempt+1, "error", err)
				continue
			}
			slog.Info("prompt sent", "vm", result.Name, "conversation", res.ConversationID)
			if v, ok := s.vmSetups.Load(result.Name); ok {
				v.(*vmSetup).Done = true
			}
			return
		}
		slog.Error("send prompt failed after retries", "vm", result.Name)
		if v, ok := s.vmSetups.Load(result.Name); ok {
			v.(*vmSetup).Done = true
		}
	}()

	return c.Redirect(http.StatusFound, "/admin/vms/"+result.Name+"/setup")
}

func (s *Server) HandleAdminVMSetup(c echo.Context) error {
	name := c.Param("name")
	v, ok := s.vmSetups.Load(name)
	if !ok {
		return c.Redirect(http.StatusFound, "/admin/vms")
	}
	setup := v.(*vmSetup)
	if setup.Done {
		s.vmSetups.Delete(name)
		return c.Redirect(http.StatusFound, setup.ShelleyURL)
	}
	return c.HTML(http.StatusOK, vmSetupPage(name))
}

func (s *Server) HandleAdminVMSetupStatus(c echo.Context) error {
	name := c.Param("name")
	v, ok := s.vmSetups.Load(name)
	if !ok {
		return c.JSON(http.StatusOK, map[string]any{"done": true, "redirect": "/admin/vms"})
	}
	setup := v.(*vmSetup)
	if setup.Done {
		s.vmSetups.Delete(name)
		return c.JSON(http.StatusOK, map[string]any{"done": true, "redirect": setup.ShelleyURL})
	}
	return c.JSON(http.StatusOK, map[string]any{"done": false})
}

func vmSetupPage(name string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Setting up ` + name + `</title>
	<link rel="stylesheet" href="/assets/css/output.css">
</head>
<body class="bg-background text-foreground min-h-screen flex items-center justify-center">
	<div class="text-center space-y-4">
		<div class="text-6xl">🐈</div>
		<h1 class="text-2xl font-bold">Setting up ` + name + `</h1>
		<p class="text-muted-foreground">Waiting for the agent to start&hellip;</p>
		<div class="inline-block h-8 w-8 animate-spin rounded-full border-4 border-current border-r-transparent"></div>
	</div>
	<script>
		(function poll() {
			fetch("/admin/vms/` + name + `/setup/status")
				.then(r => r.json())
				.then(data => {
					if (data.done) {
						window.location.href = data.redirect;
					} else {
						setTimeout(poll, 2000);
					}
				})
				.catch(() => setTimeout(poll, 2000));
		})();
	</script>
</body>
</html>`
}

func (s *Server) HandleAdminDeleteVM(c echo.Context) error {
	ctx := c.Request().Context()
	subject := c.Get("subject").(string)
	userEmail := c.Get("userEmail").(string)
	vmName := c.Param("name")

	if s.ExeDev == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "exe.dev client not configured")
	}

	if vmName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "vm name required")
	}

	info, err := s.ExeDev.ShareShow(ctx, vmName)
	if err == nil && info.Status == "public" {
		return echo.NewHTTPError(http.StatusForbidden, "cannot delete a public VM")
	}

	slog.Info("deleting vm", "name", vmName, "user", userEmail)

	if err := s.ExeDev.DeleteVM(ctx, vmName); err != nil {
		return errors.Wrap(err, "delete vm")
	}

	if s.DNS != nil {
		if err := s.DNS.DeleteCNAME(ctx, vmName); err != nil {
			slog.Error("dns delete cname", "vm", vmName, "error", err)
		}
	}

	if s.DB != nil {
		q := dbgen.New(s.DB)
		_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
			ActorID:    subject,
			ActorType:  "user",
			Action:     "deleted_vm",
			ObjectID:   vmName,
			ObjectType: "vm",
			Metadata:   &userEmail,
		})
	}

	return c.Redirect(http.StatusFound, "/admin/vms")
}

func (s *Server) HandleAdminToggleShare(c echo.Context) error {
	ctx := c.Request().Context()
	vmName := c.Param("name")

	if s.ExeDev == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "exe.dev client not configured")
	}

	action := c.FormValue("action")
	switch action {
	case "public":
		if err := s.ExeDev.ShareSetPublic(ctx, vmName); err != nil {
			return errors.Wrap(err, "share set-public")
		}
		slog.Info("exe.dev share set-public", "vm", vmName)
	case "private":
		if err := s.ExeDev.ShareSetPrivate(ctx, vmName); err != nil {
			return errors.Wrap(err, "share set-private")
		}
		slog.Info("exe.dev share set-private", "vm", vmName)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "invalid action")
	}

	return c.Redirect(http.StatusFound, "/admin/vms")
}

func buildVMPrompt(issuerURL, token, repo, registerRef string) string {
	u, _ := url.Parse(issuerURL)
	u.User = url.User(token)
	registerURL := u.String() + "/api/register"

	return fmt.Sprintf(`go install github.com/housecat-inc/go-template/cmd/register@%s
~/go/bin/register %s %s@main
`, registerRef, registerURL, repo)
}

func (s *Server) HandleResolveBranch(c echo.Context) error {
	repo := c.QueryParam("repo")
	branch := c.QueryParam("branch")
	if repo == "" || branch == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "repo and branch required")
	}

	if s.GitProxy == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "git proxy not configured")
	}

	ctx := c.Request().Context()
	sha, err := s.GitProxy.ResolveBranch(ctx, repo, branch)
	if err != nil {
		slog.Warn("resolve branch", "repo", repo, "branch", branch, "error", err)
		return c.JSON(http.StatusOK, map[string]string{"sha": ""})
	}
	return c.JSON(http.StatusOK, map[string]string{"sha": sha})
}
