package srv

import (
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
	"github.com/housecat-inc/auth/exedev"
	"github.com/housecat-inc/auth/ui/pages"
)

func (s *Server) HandleVMs(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)
	provider := c.Get("provider").(string)

	if s.DB == nil {
		return echo.NewHTTPError(500, "database not configured")
	}

	q := dbgen.New(s.DB)
	email := strings.ToLower(strings.TrimSpace(userEmail))
	domain := emailDomain(email)

	clients, err := q.ListOidcClientsByAccess(ctx, dbgen.ListOidcClientsByAccessParams{
		Email:  email,
		Domain: domain,
	})
	if err != nil {
		return errors.Wrap(err, "list clients by access")
	}

	clientByName := map[string]dbgen.OidcClient{}
	for _, cl := range clients {
		clientByName[cl.Name] = cl
	}

	var vms []pages.UserVM
	if s.ExeDev != nil {
		allVMs, err := s.ExeDev.ListVMs(ctx)
		if err != nil {
			slog.Error("list vms", "error", err)
		} else {
			for _, vm := range allVMs {
				cl, ok := clientByName[vm.Name]
				if !ok {
					continue
				}
				uvm := pages.UserVM{
					AppURL: appURL(vm, cl),
					Name:   vm.Name,
					Status: vm.Status,
				}
				vms = append(vms, uvm)
			}
		}
	}

	data := pages.VMsData{
		IsAdmin:   isAdminWithProvider(userEmail, provider),
		LogoutURL: logoutURL,
		UserEmail: userEmail,
		VMs:       vms,
	}
	return pages.VMs(data).Render(ctx, c.Response())
}

func emailDomain(email string) string {
	_, domain, _ := strings.Cut(email, "@")
	return strings.ToLower(domain)
}

func appURL(vm exedev.VM, client dbgen.OidcClient) string {
	if strings.Contains(client.RedirectUris, ".vm.housecat.io") {
		return "https://" + vm.Name + ".vm.housecat.io"
	}
	return vm.HTTPSURL
}
