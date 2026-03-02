package srv

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
	userID := c.Get("userID").(string)
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

	var activities []pages.ActivityEntry
	if s.DB != nil {
		q := dbgen.New(s.DB)
		_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
			ActorID:    userID,
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
				ActorID: userID,
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

	data := pages.AdminVMsData{
		Activities: activities,
		Configured: s.ExeDev != nil,
		Error:      vmErr,
		LogoutURL:  logoutURL,
		UserEmail:  userEmail,
		VMs:        vms,
	}
	component := pages.AdminVMs(data)
	return component.Render(r.Context(), c.Response())
}

func (s *Server) HandleAdminBrowserLink(c echo.Context) error {
	ctx := c.Request().Context()

	if s.ExeDev == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "exe.dev client not configured")
	}

	magicLink, err := s.ExeDev.BrowserLink(ctx)
	if err != nil {
		slog.Error("browser link", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate browser link")
	}

	if s.DB != nil {
		userID := c.Get("userID").(string)
		q := dbgen.New(s.DB)
		_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
			ActorID:    userID,
			ActorType:  "user",
			Action:     "browser_link",
			ObjectID:   "dashboard",
			ObjectType: "vm",
		})
	}

	return c.Redirect(http.StatusFound, magicLink)
}

func (s *Server) HandleAdminNewVM(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)

	if s.ExeDev == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "exe.dev client not configured")
	}

	token, _, err := s.createRegistrationToken(ctx, userID)
	if err != nil {
		return err
	}

	q := dbgen.New(s.DB)

	slog.Info("creating vm", "user", userEmail)

	result, err := s.ExeDev.CreateVM(ctx)
	if err != nil {
		return errors.Wrap(err, "create vm")
	}

	slog.Info("vm created", "name", result.Name, "user", userEmail)

	_ = q.InsertActivity(ctx, dbgen.InsertActivityParams{
		ActorID:    userID,
		ActorType:  "user",
		Action:     "created_vm",
		ObjectID:   result.Name,
		ObjectType: "vm",
		Metadata:   &userEmail,
	})

	issue := s.issuerURL(r)
	prompt := buildVMPrompt(issue, token)

	go func() {
		for attempt := 0; attempt < 30; attempt++ {
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			res, err := s.ExeDev.SendPrompt(context.Background(), result.Name, prompt, "claude-sonnet-4.5")
			if err != nil {
				slog.Warn("send prompt retry", "vm", result.Name, "attempt", attempt+1, "error", err)
				continue
			}
			slog.Info("prompt sent", "vm", result.Name, "conversation", res.ConversationID)
			return
		}
		slog.Error("send prompt failed after retries", "vm", result.Name)
	}()

	return c.Redirect(http.StatusFound, result.ShelleyURL)
}

func buildVMPrompt(issuerURL, token string) string {
	return fmt.Sprintf(`git clone -b main https://github.com/housecat-inc/go-template
cd go-template
go run ./cmd/register --token %s %s/register
`, token, issuerURL)
}
