package srv

import (
	"context"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/housecat-inc/auth/db/dbgen"
)

const (
	registrationScope = "client:register"
	registrationAppID = "_registration"
)

func (s *Server) createRegistrationToken(ctx context.Context, userID string) (string, time.Time, error) {
	tokenID := uuid.NewString()
	expiresAt := time.Now().UTC().Add(15 * time.Minute)

	q := dbgen.New(s.DB)
	err := q.InsertAccessToken(ctx, dbgen.InsertAccessTokenParams{
		ID:            tokenID,
		ApplicationID: registrationAppID,
		Subject:       userID,
		Audience:      "",
		Scopes:        registrationScope,
		ExpiresAt:     expiresAt,
	})
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "insert registration token")
	}
	return tokenID, expiresAt, nil
}

// HandleRegistrationToken generates a short-lived access token with
// client:register scope (an RFC 7591 Initial Access Token).
func (s *Server) HandleRegistrationToken(c echo.Context) error {
	ctx := c.Request().Context()
	userID := c.Get("userID").(string)

	tokenID, expiresAt, err := s.createRegistrationToken(ctx, userID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]any{
		"expires_at": expiresAt.Format(time.RFC3339),
		"token":      tokenID,
	})
}
