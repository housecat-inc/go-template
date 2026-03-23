package migrations

import (
	"context"
	"database/sql"
	"strings"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(Up00013, Down00013)
}

func Up00013(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id, allowed_domain, allowed_emails FROM oidc_clients WHERE allowed_domain != '' OR allowed_emails != ''")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var domain, emails string
		if err := rows.Scan(&id, &domain, &emails); err != nil {
			return err
		}
		if domain != "" {
			if _, err := tx.ExecContext(ctx, "INSERT INTO client_access (client_id, domain) VALUES (?, ?)", id, strings.ToLower(strings.TrimSpace(domain))); err != nil {
				return err
			}
		}
		if emails != "" {
			for _, email := range strings.Split(emails, ",") {
				email = strings.ToLower(strings.TrimSpace(email))
				if email == "" {
					continue
				}
				if _, err := tx.ExecContext(ctx, "INSERT INTO client_access (client_id, email) VALUES (?, ?)", id, email); err != nil {
					return err
				}
			}
		}
	}
	return rows.Err()
}

func Down00013(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM client_access")
	return err
}
