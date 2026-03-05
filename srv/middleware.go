package srv

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
)

// RequestLogger is Echo middleware that logs every HTTP request using slog.
func RequestLogger() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)

			// If the handler returned an error, let Echo's error handler
			// set the response status before we log.
			if err != nil {
				c.Error(err)
			}

			req := c.Request()
			res := c.Response()
			latency := time.Since(start)

			attrs := []any{
				"method", req.Method,
				"path", req.URL.Path,
				"status", res.Status,
				"latency", latency,
				"remote_ip", c.RealIP(),
			}
			if q := req.URL.RawQuery; q != "" {
				attrs = append(attrs, "query", q)
			}

			status := res.Status
			switch {
			case status >= 500:
				if err != nil {
					attrs = append(attrs, "error", err)
				}
				slog.Error("request", attrs...)
			case status >= 400:
				slog.Warn("request", attrs...)
			default:
				slog.Info("request", attrs...)
			}

			return nil // error already handled above
		}
	}
}
