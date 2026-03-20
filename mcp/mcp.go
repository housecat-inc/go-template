package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

var ErrTokenNotFound = errors.New("token not found")

type Connection struct {
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Level       string   `json:"level"`
	Scopes      []string `json:"scopes"`
	URL         string   `json:"url"`
}

type Service struct {
	Connections  []Connection `json:"connections"`
	Description  string       `json:"description"`
	ID           string       `json:"id"`
	Name         string       `json:"name"`
}

type ConnectionsResponse struct {
	Services []Service     `json:"services"`
	User     *UserIdentity `json:"user"`
}

type UserIdentity struct {
	Email string `json:"email"`
	ID    string `json:"id"`
}

var Services = []Service{
	{
		ID:          "attio",
		Name:        "Attio",
		Description: "CRM records, notes, and tasks via Attio MCP",
		Connections: []Connection{
			{Level: "write", Description: "Read and write CRM data", Scopes: []string{"mcp", "openid", "offline_access"}},
		},
	},
	{
		ID:          "gcal",
		Name:        "Google Calendar",
		Description: "Calendar access via Google Calendar API",
		Connections: []Connection{
			{Level: "read", Description: "Get events", Scopes: []string{"https://www.googleapis.com/auth/calendar.readonly"}},
			{Level: "draft", Description: "Create personal events for review", Scopes: []string{"https://www.googleapis.com/auth/calendar"}},
			{Level: "write", Description: "Create events and invites", Scopes: []string{"https://www.googleapis.com/auth/calendar"}},
		},
	},
	{
		ID:          "gdrive",
		Name:        "Google Drive",
		Description: "File access via Google Drive API",
		Connections: []Connection{
			{Level: "read", Description: "Get files and folders", Scopes: []string{"https://www.googleapis.com/auth/drive.readonly"}},
			{Level: "draft", Description: "Create private docs for review", Scopes: []string{"https://www.googleapis.com/auth/drive.file"}},
			{Level: "write", Description: "Create and share files", Scopes: []string{"https://www.googleapis.com/auth/drive"}},
		},
	},
	{
		ID:          "gdocs",
		Name:        "Google Docs",
		Description: "Document access via Google Docs API",
		Connections: []Connection{
			{Level: "read", Description: "Get documents", Scopes: []string{"https://www.googleapis.com/auth/documents.readonly"}},
			{Level: "draft", Description: "Create and edit own documents", Scopes: []string{"https://www.googleapis.com/auth/documents"}},
			{Level: "write", Description: "Edit any accessible document", Scopes: []string{"https://www.googleapis.com/auth/documents"}},
		},
	},
	{
		ID:          "gmail",
		Name:        "Google Mail",
		Description: "Email access via Gmail API",
		Connections: []Connection{
			{Level: "read", Description: "Get emails and threads", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}},
			{Level: "draft", Description: "Create draft emails for review", Scopes: []string{"https://www.googleapis.com/auth/gmail.compose"}},
			{Level: "write", Description: "Send emails and manage labels", Scopes: []string{"https://www.googleapis.com/auth/gmail.send", "https://www.googleapis.com/auth/gmail.labels", "https://www.googleapis.com/auth/gmail.modify"}},
		},
	},
	{
		ID:          "gsheets",
		Name:        "Google Sheets",
		Description: "Spreadsheet access via Google Sheets API",
		Connections: []Connection{
			{Level: "read", Description: "Get spreadsheets", Scopes: []string{"https://www.googleapis.com/auth/spreadsheets.readonly"}},
			{Level: "draft", Description: "Create and edit own spreadsheets", Scopes: []string{"https://www.googleapis.com/auth/spreadsheets"}},
			{Level: "write", Description: "Edit any accessible spreadsheet", Scopes: []string{"https://www.googleapis.com/auth/spreadsheets"}},
		},
	},
	{
		ID:          "granola",
		Name:        "Granola",
		Description: "Meeting notes via Granola MCP",
		Connections: []Connection{
			{Level: "read", Description: "Get meeting notes", Scopes: []string{"openid", "email", "offline_access"}},
		},
	},
	{
		ID:          "notion",
		Name:        "Notion",
		Description: "Pages and databases via Notion MCP",
		Connections: []Connection{
			{Level: "write", Description: "Read and write pages and databases", Scopes: []string{"openid", "email", "offline_access"}},
		},
	},
	{
		ID:          "slack",
		Name:        "Slack",
		Description: "Messaging via Slack API",
		Connections: []Connection{
			{Level: "read", Description: "Get messages and channels", Scopes: []string{"canvases:read", "channels:history", "channels:read", "groups:history", "groups:read", "search:read", "users:read"}},
			{Level: "draft", Description: "Create DMs to self for review", Scopes: []string{"canvases:read", "canvases:write", "channels:history", "channels:read", "chat:write", "groups:history", "groups:read", "im:write", "search:read", "users:read"}},
			{Level: "write", Description: "Send messages to channels", Scopes: []string{"canvases:read", "canvases:write", "channels:history", "channels:read", "chat:write", "groups:history", "groups:read", "im:write", "search:read", "users:read"}},
		},
	},
}

type BrandingLookup func(ctx context.Context, subject string) bool

type ConnectionsLookup func(ctx context.Context, subject string) map[string]map[string]bool

type TokenLookup func(ctx context.Context, subject, service, level string) (string, error)

func subjectFromRequest(req *gomcp.CallToolRequest) string {
	if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
		return extra.TokenInfo.UserID
	}
	return ""
}

func userFromRequest(req *gomcp.CallToolRequest) *UserIdentity {
	extra := req.GetExtra()
	if extra == nil || extra.TokenInfo == nil {
		return nil
	}
	u := &UserIdentity{ID: extra.TokenInfo.UserID}
	if email, ok := extra.TokenInfo.Extra["email"].(string); ok {
		u.Email = email
	}
	return u
}

func levelFallback(minLevel string) []string {
	switch minLevel {
	case "read":
		return []string{"read", "draft", "write"}
	case "draft":
		return []string{"draft", "write"}
	default:
		return []string{minLevel}
	}
}

func tokenForService(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, service, minLevel string) (string, error) {
	subject := subjectFromRequest(req)
	if subject == "" {
		return "", errors.New("not authenticated")
	}

	for _, level := range levelFallback(minLevel) {
		token, err := lookup(ctx, subject, service, level)
		if err != nil {
			if errors.Is(err, ErrTokenNotFound) {
				continue
			}
			return "", errors.Wrap(err, "token lookup")
		}
		if token != "" {
			return token, nil
		}
	}
	return "", errors.Newf("%s not connected — connect at %s level or higher via the connections page", service, minLevel)
}

func gcalClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*GCalClient, error) {
	token, err := tokenForService(ctx, req, lookup, "gcal", minLevel)
	if err != nil {
		return nil, err
	}
	return &GCalClient{Token: token}, nil
}

func gdocsClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*GDocsClient, error) {
	token, err := tokenForService(ctx, req, lookup, "gdocs", minLevel)
	if err != nil {
		return nil, err
	}
	return &GDocsClient{Token: token}, nil
}

func gdriveClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*GDriveClient, error) {
	token, err := tokenForService(ctx, req, lookup, "gdrive", minLevel)
	if err != nil {
		return nil, err
	}
	return &GDriveClient{Token: token}, nil
}

func gmailClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*GmailClient, error) {
	token, err := tokenForService(ctx, req, lookup, "gmail", minLevel)
	if err != nil {
		return nil, err
	}
	return &GmailClient{Token: token}, nil
}

func gsheetsClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*GSheetsClient, error) {
	token, err := tokenForService(ctx, req, lookup, "gsheets", minLevel)
	if err != nil {
		return nil, err
	}
	return &GSheetsClient{Token: token}, nil
}

func attioClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*AttioClient, error) {
	token, err := tokenForService(ctx, req, lookup, "attio", minLevel)
	if err != nil {
		return nil, err
	}
	return &AttioClient{Token: token}, nil
}

func granolaClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*GranolaClient, error) {
	token, err := tokenForService(ctx, req, lookup, "granola", minLevel)
	if err != nil {
		return nil, err
	}
	return &GranolaClient{Token: token}, nil
}

func notionClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*NotionClient, error) {
	token, err := tokenForService(ctx, req, lookup, "notion", minLevel)
	if err != nil {
		return nil, err
	}
	return &NotionClient{Token: token}, nil
}

func slackClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*SlackClient, error) {
	token, err := tokenForService(ctx, req, lookup, "slack", minLevel)
	if err != nil {
		return nil, err
	}
	return &SlackClient{Token: token}, nil
}

func textResult(v any) (*gomcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{
			&gomcp.TextContent{Text: string(data)},
		},
	}, nil
}

func errResult(msg string) (*gomcp.CallToolResult, any, error) {
	slog.Warn("mcp tool error", "error", msg)
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{
			&gomcp.TextContent{Text: msg},
		},
		IsError: true,
	}, nil, nil
}

// UpstreamTool is a tool definition fetched from an upstream MCP server.
type UpstreamTool struct {
	Service     string          // "attio", "granola", or "notion"
	Name        string          // upstream tool name (e.g. "notion-search")
	Description string          // tool description
	InputSchema json.RawMessage // exact upstream JSON schema
}

// requestIDMiddleware is server-side middleware that assigns a request ID to
// every tools/call request. If the client sends a "request_id" in _meta, it is
// preserved; otherwise the server generates a UUIDv4. The request ID is echoed
// back on the result's _meta and included in structured log output. Empty tool
// results are given a fallback message so every call produces visible output.
func requestIDMiddleware() gomcp.Middleware {
	return func(next gomcp.MethodHandler) gomcp.MethodHandler {
		return func(ctx context.Context, method string, req gomcp.Request) (gomcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}

			// Extract or generate request ID.
			var requestID string
			if meta := req.GetParams().GetMeta(); meta != nil {
				if rid, ok := meta["request_id"].(string); ok && rid != "" {
					requestID = rid
				}
			}
			if requestID == "" {
				requestID = uuid.NewString()
			}

			// Extract tool name for logging.
			var toolName string
			if params, ok := req.GetParams().(*gomcp.CallToolParamsRaw); ok {
				toolName = params.Name
			}

			slog.Info("tool call start", "request_id", requestID, "tool", toolName)

			result, err := next(ctx, method, req)

			// Stamp request ID and ensure non-empty content on CallToolResult.
			if ctr, ok := result.(*gomcp.CallToolResult); ok && ctr != nil {
				meta := ctr.GetMeta()
				if meta == nil {
					meta = map[string]any{}
				}
				meta["request_id"] = requestID
				ctr.SetMeta(meta)

				if len(ctr.Content) == 0 {
					ctr.Content = []gomcp.Content{
						&gomcp.TextContent{Text: "completed successfully (no output)"},
					}
				}
			}

			if err != nil {
				slog.Warn("tool call error", "request_id", requestID, "tool", toolName, "error", err)
			} else {
				isErr := false
				if ctr, ok := result.(*gomcp.CallToolResult); ok && ctr != nil {
					isErr = ctr.IsError
				}
				slog.Info("tool call done", "request_id", requestID, "tool", toolName, "is_error", isErr)
			}

			return result, err
		}
	}
}

func refererFromRequest(req *gomcp.CallToolRequest) string {
	if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
		if ref, ok := extra.TokenInfo.Extra["referer"].(string); ok {
			return ref
		}
	}
	return ""
}

func ensureScheme(u string) string {
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		return "https://" + u
	}
	return u
}

func brandingFooter(ctx context.Context, req *gomcp.CallToolRequest, branding BrandingLookup, baseURL, verb, format string) string {
	if branding == nil {
		return ""
	}
	subject := subjectFromRequest(req)
	if subject == "" || !branding(ctx, subject) {
		return ""
	}
	link := ensureScheme(refererFromRequest(req))
	if link == "" {
		link = ensureScheme(baseURL)
	}
	text := verb + " with Housecat"
	switch format {
	case "html":
		if link != "" {
			return `<br><br><small><a href="` + link + `">` + text + `</a></small>`
		}
		return "<br><br><small>" + text + "</small>"
	case "slack":
		if link != "" {
			return "\n\n_<" + link + "|" + text + ">_"
		}
		return "\n\n_" + text + "_"
	default:
		if link != "" {
			return "\n\n" + text + " (" + link + ")"
		}
		return "\n\n" + text
	}
}

func NewServer(baseURL string, lookup TokenLookup, connLookup ConnectionsLookup, branding BrandingLookup, upstreamTools []UpstreamTool) *gomcp.Server {
	server := gomcp.NewServer(&gomcp.Implementation{
		Name:    "housecat",
		Version: "0.1.0",
	}, nil)

	server.AddReceivingMiddleware(requestIDMiddleware())

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "connections",
		Description: "List integration connection statuses for Gmail, Google Calendar, Google Docs, Google Drive, Google Sheets, Slack, Granola, and Notion",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct{}) (*gomcp.CallToolResult, any, error) {
		services := make([]Service, len(Services))
		for i, svc := range Services {
			services[i] = svc
			conns := make([]Connection, len(svc.Connections))
			for j, conn := range svc.Connections {
				conns[j] = conn
				conns[j].URL = baseURL + "/connect/" + svc.ID + "/enable/" + conn.Level
			}
			services[i].Connections = conns
		}

		var user *UserIdentity
		if u := userFromRequest(req); u != nil {
			user = u
			if connLookup != nil {
				connected := connLookup(ctx, u.ID)
				for i := range services {
					if svcLevels, ok := connected[services[i].ID]; ok {
						for j := range services[i].Connections {
							services[i].Connections[j].Enabled = svcLevels[services[i].Connections[j].Level]
						}
					}
				}
			}
		}

		resp := ConnectionsResponse{Services: services, User: user}
		result, err := textResult(resp)
		return result, nil, err
	})

	// Google Calendar tools

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gcal_list_calendars",
		Description: "List all calendars the user has access to. Requires Google Calendar read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct{}) (*gomcp.CallToolResult, any, error) {
		client, err := gcalClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ListCalendars(ctx)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gcal_list_events",
		Description: "List events from a Google Calendar. Supports time range filtering and text search. Requires Google Calendar read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		CalendarID string `json:"calendar_id,omitempty" jsonschema:"Calendar ID (default 'primary')"`
		MaxResults int    `json:"max_results,omitempty" jsonschema:"Max events to return (default 25)"`
		PageToken  string `json:"page_token,omitempty" jsonschema:"Pagination token from previous response"`
		Query      string `json:"query,omitempty" jsonschema:"Text search query"`
		TimeMax    string `json:"time_max,omitempty" jsonschema:"Upper bound (RFC3339) for event start time"`
		TimeMin    string `json:"time_min,omitempty" jsonschema:"Lower bound (RFC3339) for event start time"`
	}) (*gomcp.CallToolResult, any, error) {
		client, err := gcalClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ListEvents(ctx, input.CalendarID, input.TimeMin, input.TimeMax, input.MaxResults, input.PageToken, input.Query)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gcal_get_event",
		Description: "Get details of a specific Google Calendar event. Requires Google Calendar read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		CalendarID string `json:"calendar_id,omitempty" jsonschema:"Calendar ID (default 'primary')"`
		EventID    string `json:"event_id" jsonschema:"Event ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.EventID == "" {
			return errResult("event_id is required")
		}
		client, err := gcalClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetEvent(ctx, input.CalendarID, input.EventID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gcal_create_event",
		Description: "Create a new Google Calendar event with attendees, location, and description. Requires Google Calendar draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Attendees   []string `json:"attendees,omitempty" jsonschema:"List of attendee email addresses"`
		CalendarID  string   `json:"calendar_id,omitempty" jsonschema:"Calendar ID (default 'primary')"`
		Description string   `json:"description,omitempty" jsonschema:"Event description"`
		End         string   `json:"end" jsonschema:"End time in RFC3339 format"`
		Location    string   `json:"location,omitempty" jsonschema:"Event location"`
		Start       string   `json:"start" jsonschema:"Start time in RFC3339 format"`
		Summary     string   `json:"summary" jsonschema:"Event title"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Summary == "" {
			return errResult("summary is required")
		}
		if input.Start == "" {
			return errResult("start is required")
		}
		if input.End == "" {
			return errResult("end is required")
		}
		client, err := gcalClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		desc := input.Description + brandingFooter(ctx, req, branding, baseURL, "Created", "html")
		out, err := client.CreateEvent(ctx, input.CalendarID, input.Summary, desc, input.Start, input.End, input.Attendees, input.Location)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gcal_quick_add",
		Description: "Create a Google Calendar event from natural language text (e.g. 'Lunch with John tomorrow at noon'). Requires Google Calendar draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		CalendarID string `json:"calendar_id,omitempty" jsonschema:"Calendar ID (default 'primary')"`
		Text       string `json:"text" jsonschema:"Natural language event description"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Text == "" {
			return errResult("text is required")
		}
		client, err := gcalClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.QuickAdd(ctx, input.CalendarID, input.Text)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	// Google Docs tools

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdocs_get_document",
		Description: "Get the text content of a Google Doc by document ID. Requires Google Docs read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		DocumentID string `json:"document_id" jsonschema:"Google Docs document ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.DocumentID == "" {
			return errResult("document_id is required")
		}
		client, err := gdocsClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetDocument(ctx, input.DocumentID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdocs_create_document",
		Description: "Create a new Google Doc with the given title. Requires Google Docs draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Title string `json:"title" jsonschema:"Document title"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Title == "" {
			return errResult("title is required")
		}
		client, err := gdocsClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.CreateDocument(ctx, input.Title)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdocs_insert_text",
		Description: "Insert text into a Google Doc at a specific index. Index 1 is the beginning of the document. Requires Google Docs draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		DocumentID string `json:"document_id" jsonschema:"Google Docs document ID"`
		Index      int    `json:"index,omitempty" jsonschema:"Character index to insert at (default 1, beginning of doc)"`
		Text       string `json:"text" jsonschema:"Text to insert"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.DocumentID == "" {
			return errResult("document_id is required")
		}
		if input.Text == "" {
			return errResult("text is required")
		}
		client, err := gdocsClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.InsertText(ctx, input.DocumentID, input.Text, input.Index)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	// Google Drive tools

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdrive_list_files",
		Description: "List files in Google Drive. Supports Google Drive search syntax (e.g. \"name contains 'report'\" or \"mimeType='application/pdf'\"). Requires Google Drive read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		OrderBy   string `json:"order_by,omitempty" jsonschema:"Sort order (default 'modifiedTime desc')"`
		PageSize  int    `json:"page_size,omitempty" jsonschema:"Max files to return (default 10)"`
		PageToken string `json:"page_token,omitempty" jsonschema:"Pagination token from previous response"`
		Query     string `json:"query,omitempty" jsonschema:"Google Drive search query"`
	}) (*gomcp.CallToolResult, any, error) {
		client, err := gdriveClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ListFiles(ctx, input.Query, input.PageSize, input.PageToken, input.OrderBy)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdrive_get_file",
		Description: "Get metadata for a Google Drive file by ID. Requires Google Drive read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		FileID string `json:"file_id" jsonschema:"Google Drive file ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.FileID == "" {
			return errResult("file_id is required")
		}
		client, err := gdriveClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetFile(ctx, input.FileID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdrive_read_file",
		Description: "Read the text content of a Google Drive file. Supports Google Docs, Sheets (as CSV), Slides, and plain text files. Requires Google Drive read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		FileID string `json:"file_id" jsonschema:"Google Drive file ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.FileID == "" {
			return errResult("file_id is required")
		}
		client, err := gdriveClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetFileContent(ctx, input.FileID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdrive_create_file",
		Description: "Create a new text file in Google Drive. Requires Google Drive draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Content  string `json:"content" jsonschema:"File text content"`
		MimeType string `json:"mime_type,omitempty" jsonschema:"MIME type (default 'text/plain')"`
		Name     string `json:"name" jsonschema:"File name"`
		ParentID string `json:"parent_id,omitempty" jsonschema:"Parent folder ID (optional)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Name == "" {
			return errResult("name is required")
		}
		if input.Content == "" {
			return errResult("content is required")
		}
		if input.MimeType == "" {
			input.MimeType = "text/plain"
		}
		client, err := gdriveClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.CreateFile(ctx, input.Name, input.MimeType, input.Content, input.ParentID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdrive_list_permissions",
		Description: "List who has access to a Google Drive file. Requires Google Drive read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		FileID string `json:"file_id" jsonschema:"Google Drive file ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.FileID == "" {
			return errResult("file_id is required")
		}
		client, err := gdriveClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ListPermissions(ctx, input.FileID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gdrive_add_permission",
		Description: "Share a Google Drive file with someone by email. Requires Google Drive write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Email  string `json:"email" jsonschema:"Email address to share with"`
		FileID string `json:"file_id" jsonschema:"Google Drive file ID"`
		Role   string `json:"role" jsonschema:"Permission role: reader, writer, or commenter"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.FileID == "" {
			return errResult("file_id is required")
		}
		if input.Email == "" {
			return errResult("email is required")
		}
		if input.Role == "" {
			return errResult("role is required")
		}
		if input.Role != "reader" && input.Role != "writer" && input.Role != "commenter" {
			return errResult("role must be one of: reader, writer, commenter")
		}
		client, err := gdriveClientFromRequest(ctx, req, lookup, "write")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.AddPermission(ctx, input.FileID, input.Email, input.Role)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	// Gmail tools

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gmail_get_profile",
		Description: "Get the current Gmail user's profile (email address, history ID, message/thread counts). Requires Gmail read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct{}) (*gomcp.CallToolResult, any, error) {
		client, err := gmailClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetProfile(ctx)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gmail_search_messages",
		Description: "Search Gmail messages. Uses Gmail search syntax (e.g. 'from:user@example.com subject:hello is:unread'). Requires Gmail read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		IncludeSpamTrash bool   `json:"include_spam_trash,omitempty" jsonschema:"Include spam and trash in results (default false)"`
		MaxResults       int    `json:"max_results,omitempty" jsonschema:"Max messages to return (default 20, max 500)"`
		PageToken        string `json:"page_token,omitempty" jsonschema:"Pagination token from previous response"`
		Query            string `json:"query,omitempty" jsonschema:"Gmail search query (e.g. 'from:user@example.com subject:hello is:unread')"`
	}) (*gomcp.CallToolResult, any, error) {
		client, err := gmailClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.SearchMessages(ctx, input.Query, input.MaxResults, input.PageToken, input.IncludeSpamTrash)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gmail_read_message",
		Description: "Read the full content of a Gmail message by ID. Requires Gmail read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		MessageID string `json:"message_id" jsonschema:"Gmail message ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.MessageID == "" {
			return errResult("messageId is required")
		}
		client, err := gmailClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ReadMessage(ctx, input.MessageID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gmail_read_thread",
		Description: "Get all messages in a Gmail thread by thread ID. Requires Gmail read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ThreadID string `json:"thread_id" jsonschema:"Gmail thread ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.ThreadID == "" {
			return errResult("threadId is required")
		}
		client, err := gmailClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetThread(ctx, input.ThreadID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gmail_list_drafts",
		Description: "List Gmail drafts. Requires Gmail read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		MaxResults int    `json:"max_results,omitempty" jsonschema:"Max drafts to return (default 20)"`
		PageToken  string `json:"page_token,omitempty" jsonschema:"Pagination token from previous response"`
	}) (*gomcp.CallToolResult, any, error) {
		client, err := gmailClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ListDrafts(ctx, input.MaxResults, input.PageToken)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gmail_create_draft",
		Description: "Create a draft email for review before sending. Requires Gmail draft connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Bcc         string `json:"bcc,omitempty" jsonschema:"BCC recipient email address"`
		Body        string `json:"body" jsonschema:"Email body text"`
		Cc          string `json:"cc,omitempty" jsonschema:"CC recipient email address"`
		ContentType string `json:"content_type,omitempty" jsonschema:"MIME type for body (default text/plain)"`
		Subject     string `json:"subject,omitempty" jsonschema:"Email subject line (required unless threadId is provided)"`
		ThreadID    string `json:"thread_id,omitempty" jsonschema:"Thread ID to associate draft with"`
		To          string `json:"to" jsonschema:"Recipient email address"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.To == "" {
			return errResult("to is required")
		}
		if input.Body == "" {
			return errResult("body is required")
		}
		if input.Subject == "" && input.ThreadID == "" {
			return errResult("subject is required unless threadId is provided")
		}
		client, err := gmailClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		body := input.Body + brandingFooter(ctx, req, branding, baseURL, "Sent", "html")
		out, err := client.CreateDraft(ctx, CreateDraftIn{
			Bcc:         input.Bcc,
			Body:        body,
			Cc:          input.Cc,
			ContentType: input.ContentType,
			Subject:     input.Subject,
			ThreadID:    input.ThreadID,
			To:          input.To,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gmail_list_labels",
		Description: "List all Gmail labels (inbox, sent, custom labels, etc.). Requires Gmail read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct{}) (*gomcp.CallToolResult, any, error) {
		client, err := gmailClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ListLabels(ctx)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gmail_send_message",
		Description: "Send an email via Gmail. Either send an existing draft by draftId, or compose a new message with to+body. Requires Gmail write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Bcc         string `json:"bcc,omitempty" jsonschema:"BCC recipients, comma-separated"`
		Body        string `json:"body,omitempty" jsonschema:"Email body content (required without draftId)"`
		Cc          string `json:"cc,omitempty" jsonschema:"CC recipients, comma-separated"`
		ContentType string `json:"content_type,omitempty" jsonschema:"text/plain or text/html (default text/plain)"`
		DraftID     string `json:"draft_id,omitempty" jsonschema:"Draft ID to send (provide this OR to+body)"`
		Subject     string `json:"subject,omitempty" jsonschema:"Subject line (required without draftId unless threadId provided)"`
		ThreadID    string `json:"thread_id,omitempty" jsonschema:"Thread ID to send as a reply"`
		To          string `json:"to,omitempty" jsonschema:"Recipient(s), comma-separated (required without draftId)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.DraftID != "" {
			if input.To != "" || input.Body != "" {
				return errResult("provide draftId OR to+body, not both")
			}
			client, err := gmailClientFromRequest(ctx, req, lookup, "write")
			if err != nil {
				return errResult(err.Error())
			}
			out, err := client.SendDraft(ctx, input.DraftID)
			if err != nil {
				return errResult(err.Error())
			}
			result, err := textResult(out)
			return result, nil, err
		}
		if input.To == "" {
			return errResult("to is required when draftId is not provided")
		}
		if input.Body == "" {
			return errResult("body is required when draftId is not provided")
		}
		if input.Subject == "" && input.ThreadID == "" {
			return errResult("subject is required unless threadId is provided")
		}
		client, err := gmailClientFromRequest(ctx, req, lookup, "write")
		if err != nil {
			return errResult(err.Error())
		}
		sendBody := input.Body + brandingFooter(ctx, req, branding, baseURL, "Sent", "html")
		out, err := client.SendMessage(ctx, SendMessageIn{
			Bcc:         input.Bcc,
			Body:        sendBody,
			Cc:          input.Cc,
			ContentType: input.ContentType,
			Subject:     input.Subject,
			ThreadID:    input.ThreadID,
			To:          input.To,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	// Google Sheets tools

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gsheets_get_spreadsheet",
		Description: "Get metadata for a Google Sheets spreadsheet (title, sheet names). Requires Google Sheets read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		SpreadsheetID string `json:"spreadsheet_id" jsonschema:"Google Sheets spreadsheet ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.SpreadsheetID == "" {
			return errResult("spreadsheet_id is required")
		}
		client, err := gsheetsClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetSpreadsheet(ctx, input.SpreadsheetID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gsheets_get_values",
		Description: "Read cell values from a Google Sheets range (e.g. 'Sheet1!A1:D10'). Requires Google Sheets read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Range         string `json:"range" jsonschema:"A1 notation range (e.g. 'Sheet1!A1:D10')"`
		SpreadsheetID string `json:"spreadsheet_id" jsonschema:"Google Sheets spreadsheet ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.SpreadsheetID == "" {
			return errResult("spreadsheet_id is required")
		}
		if input.Range == "" {
			return errResult("range is required")
		}
		client, err := gsheetsClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetValues(ctx, input.SpreadsheetID, input.Range)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gsheets_update_values",
		Description: "Write values to a Google Sheets range. Requires Google Sheets draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Range         string     `json:"range" jsonschema:"A1 notation range (e.g. 'Sheet1!A1:D10')"`
		SpreadsheetID string     `json:"spreadsheet_id" jsonschema:"Google Sheets spreadsheet ID"`
		Values        [][]string `json:"values" jsonschema:"2D array of cell values"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.SpreadsheetID == "" {
			return errResult("spreadsheet_id is required")
		}
		if input.Range == "" {
			return errResult("range is required")
		}
		if len(input.Values) == 0 {
			return errResult("values is required")
		}
		client, err := gsheetsClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.UpdateValues(ctx, input.SpreadsheetID, input.Range, input.Values)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gsheets_append_values",
		Description: "Append rows to a Google Sheets range. Requires Google Sheets draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Range         string     `json:"range" jsonschema:"A1 notation range to append after (e.g. 'Sheet1!A:D')"`
		SpreadsheetID string     `json:"spreadsheet_id" jsonschema:"Google Sheets spreadsheet ID"`
		Values        [][]string `json:"values" jsonschema:"2D array of row values to append"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.SpreadsheetID == "" {
			return errResult("spreadsheet_id is required")
		}
		if input.Range == "" {
			return errResult("range is required")
		}
		if len(input.Values) == 0 {
			return errResult("values is required")
		}
		client, err := gsheetsClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.AppendValues(ctx, input.SpreadsheetID, input.Range, input.Values)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gsheets_create_spreadsheet",
		Description: "Create a new Google Sheets spreadsheet. Requires Google Sheets draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Title string `json:"title" jsonschema:"Spreadsheet title"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Title == "" {
			return errResult("title is required")
		}
		client, err := gsheetsClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.CreateSpreadsheet(ctx, input.Title)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gsheets_add_sheet",
		Description: "Add a new sheet (tab) to an existing Google Sheets spreadsheet. Requires Google Sheets draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		SpreadsheetID string `json:"spreadsheet_id" jsonschema:"Google Sheets spreadsheet ID"`
		Title         string `json:"title" jsonschema:"Name for the new sheet tab"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.SpreadsheetID == "" {
			return errResult("spreadsheet_id is required")
		}
		if input.Title == "" {
			return errResult("title is required")
		}
		client, err := gsheetsClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.AddSheet(ctx, input.SpreadsheetID, input.Title)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "gsheets_rename_sheet",
		Description: "Rename a sheet (tab) in a Google Sheets spreadsheet. Use gsheets_get_spreadsheet to find sheet IDs. Requires Google Sheets draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		SheetID       int    `json:"sheet_id" jsonschema:"Sheet ID (0 for the first sheet)"`
		SpreadsheetID string `json:"spreadsheet_id" jsonschema:"Google Sheets spreadsheet ID"`
		Title         string `json:"title" jsonschema:"New name for the sheet tab"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.SpreadsheetID == "" {
			return errResult("spreadsheet_id is required")
		}
		if input.Title == "" {
			return errResult("title is required")
		}
		client, err := gsheetsClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.RenameSheet(ctx, input.SpreadsheetID, input.SheetID, input.Title)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	// Upstream MCP tools (Attio, Granola, Notion)
	// Registered dynamically from tool definitions fetched at startup.

	type mcpClientFunc func(ctx context.Context, req *gomcp.CallToolRequest) (interface {
		CallTool(ctx context.Context, toolName string, arguments map[string]any) (json.RawMessage, error)
	}, error)

	serviceClients := map[string]mcpClientFunc{
		"attio": func(ctx context.Context, req *gomcp.CallToolRequest) (interface {
			CallTool(ctx context.Context, toolName string, arguments map[string]any) (json.RawMessage, error)
		}, error) {
			return attioClientFromRequest(ctx, req, lookup, "write")
		},
		"granola": func(ctx context.Context, req *gomcp.CallToolRequest) (interface {
			CallTool(ctx context.Context, toolName string, arguments map[string]any) (json.RawMessage, error)
		}, error) {
			return granolaClientFromRequest(ctx, req, lookup, "read")
		},
		"notion": func(ctx context.Context, req *gomcp.CallToolRequest) (interface {
			CallTool(ctx context.Context, toolName string, arguments map[string]any) (json.RawMessage, error)
		}, error) {
			return notionClientFromRequest(ctx, req, lookup, "write")
		},
	}

	for _, ut := range upstreamTools {
		ut := ut // capture
		clientFn, ok := serviceClients[ut.Service]
		if !ok {
			continue
		}
		server.AddTool(&gomcp.Tool{
			Name:        ut.Name,
			Description: ut.Description,
			InputSchema: ut.InputSchema,
		}, func(ctx context.Context, req *gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			client, err := clientFn(ctx, req)
			if err != nil {
				return &gomcp.CallToolResult{
					Content: []gomcp.Content{&gomcp.TextContent{Text: err.Error()}},
					IsError: true,
				}, nil
			}
			var args map[string]any
			if req.Params.Arguments != nil {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return &gomcp.CallToolResult{
						Content: []gomcp.Content{&gomcp.TextContent{Text: "invalid arguments: " + err.Error()}},
						IsError: true,
					}, nil
				}
			}
			if args == nil {
				args = map[string]any{}
			}
			out, err := client.CallTool(ctx, ut.Name, args)
			if err != nil {
				return &gomcp.CallToolResult{
					Content: []gomcp.Content{&gomcp.TextContent{Text: err.Error()}},
					IsError: true,
				}, nil
			}
			return &gomcp.CallToolResult{
				Content: []gomcp.Content{&gomcp.TextContent{Text: string(out)}},
			}, nil
		})
	}

	// Slack tools

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_read_channel",
		Description: "Read messages from a Slack channel, group, or IM. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ChannelID      string `json:"channel" jsonschema:"Channel/group/IM ID"`
		Cursor         string `json:"cursor,omitempty" jsonschema:"Pagination cursor"`
		Latest         string `json:"latest,omitempty" jsonschema:"End of time range (timestamp)"`
		Limit          int    `json:"limit,omitempty" jsonschema:"Messages to return, 1-100 (default 100)"`
		Oldest         string `json:"oldest,omitempty" jsonschema:"Start of time range (timestamp)"`
		ResponseFormat string `json:"response_format,omitempty" jsonschema:"detailed or concise (default detailed)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.ChannelID == "" {
			return errResult("channel is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ReadChannel(ctx, ReadChannelIn{
			ChannelID:      input.ChannelID,
			Cursor:         input.Cursor,
			Latest:         input.Latest,
			Limit:          input.Limit,
			Oldest:         input.Oldest,
			ResponseFormat: input.ResponseFormat,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_read_thread",
		Description: "Read replies in a Slack thread. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ChannelID      string `json:"channel" jsonschema:"Channel ID"`
		Cursor         string `json:"cursor,omitempty" jsonschema:"Pagination cursor"`
		Latest         string `json:"latest,omitempty" jsonschema:"End of time range"`
		Limit          int    `json:"limit,omitempty" jsonschema:"Messages to return, 1-1000 (default 100)"`
		MessageTS      string `json:"message_ts" jsonschema:"Parent message timestamp"`
		Oldest         string `json:"oldest,omitempty" jsonschema:"Start of time range"`
		ResponseFormat string `json:"response_format,omitempty" jsonschema:"detailed or concise (default detailed)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.ChannelID == "" {
			return errResult("channel is required")
		}
		if input.MessageTS == "" {
			return errResult("message_ts is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ReadThread(ctx, ReadThreadIn{
			ChannelID:      input.ChannelID,
			Cursor:         input.Cursor,
			Latest:         input.Latest,
			Limit:          input.Limit,
			MessageTS:      input.MessageTS,
			Oldest:         input.Oldest,
			ResponseFormat: input.ResponseFormat,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_read_canvas",
		Description: "Read a Slack canvas by ID. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		CanvasID string `json:"canvas_id" jsonschema:"Canvas ID"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.CanvasID == "" {
			return errResult("canvas_id is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ReadCanvas(ctx, input.CanvasID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_read_user_profile",
		Description: "Get a Slack user's profile. Defaults to current user if user_id omitted. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		IncludeLocale  bool   `json:"include_locale,omitempty" jsonschema:"Include locale info"`
		ResponseFormat string `json:"response_format,omitempty" jsonschema:"detailed or concise (default detailed)"`
		UserID         string `json:"user_id,omitempty" jsonschema:"Slack user ID (default current user)"`
	}) (*gomcp.CallToolResult, any, error) {
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ReadUserProfile(ctx, ReadUserProfileIn{
			IncludeLocale:  input.IncludeLocale,
			ResponseFormat: input.ResponseFormat,
			UserID:         input.UserID,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_search_channels",
		Description: "Search Slack channels by name or topic. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ChannelTypes    string `json:"channel_types,omitempty" jsonschema:"Comma-separated: public_channel, private_channel (default public_channel)"`
		Cursor          string `json:"cursor,omitempty" jsonschema:"Pagination cursor"`
		IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"Include archived channels"`
		Limit           int    `json:"limit,omitempty" jsonschema:"Results to return, max 20 (default 20)"`
		Query           string `json:"query" jsonschema:"Search query for channels"`
		ResponseFormat  string `json:"response_format,omitempty" jsonschema:"detailed or concise (default detailed)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Query == "" {
			return errResult("query is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.SearchChannels(ctx, SearchChannelsIn{
			ChannelTypes:    input.ChannelTypes,
			Cursor:          input.Cursor,
			IncludeArchived: input.IncludeArchived,
			Limit:           input.Limit,
			Query:           input.Query,
			ResponseFormat:  input.ResponseFormat,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_search_users",
		Description: "Search Slack users by name, email, or profile attributes. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Cursor         string `json:"cursor,omitempty" jsonschema:"Pagination cursor"`
		Limit          int    `json:"limit,omitempty" jsonschema:"Results to return, max 20 (default 20)"`
		Query          string `json:"query" jsonschema:"Name, email, or profile attributes"`
		ResponseFormat string `json:"response_format,omitempty" jsonschema:"detailed or concise (default detailed)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Query == "" {
			return errResult("query is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.SearchUsers(ctx, SearchUsersIn{
			Cursor:         input.Cursor,
			Limit:          input.Limit,
			Query:          input.Query,
			ResponseFormat: input.ResponseFormat,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_search_public",
		Description: "Search public Slack messages and files. Uses Slack search syntax (e.g. 'in:#general from:@user keyword'). Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		After            string `json:"after,omitempty" jsonschema:"Unix timestamp lower bound"`
		Before           string `json:"before,omitempty" jsonschema:"Unix timestamp upper bound"`
		ContentTypes     string `json:"content_types,omitempty" jsonschema:"Comma-separated: messages, files"`
		ContextChannelID string `json:"context_channel_id,omitempty" jsonschema:"Boost results for this channel"`
		Cursor           string `json:"cursor,omitempty" jsonschema:"Pagination cursor"`
		IncludeBots      bool   `json:"include_bots,omitempty" jsonschema:"Include bot messages"`
		IncludeContext   *bool  `json:"include_context,omitempty" jsonschema:"Include surrounding context (default true)"`
		Limit            int    `json:"limit,omitempty" jsonschema:"Results, max 20 (default 20)"`
		MaxContextLength int    `json:"max_context_length,omitempty" jsonschema:"Max chars per context message"`
		Query            string `json:"query" jsonschema:"Search query with modifiers"`
		ResponseFormat   string `json:"response_format,omitempty" jsonschema:"detailed or concise (default detailed)"`
		Sort             string `json:"sort,omitempty" jsonschema:"score or timestamp (default score)"`
		SortDir          string `json:"sort_dir,omitempty" jsonschema:"asc or desc (default desc)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Query == "" {
			return errResult("query is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.SearchPublic(ctx, SearchPublicIn{
			After:            input.After,
			Before:           input.Before,
			ContentTypes:     input.ContentTypes,
			ContextChannelID: input.ContextChannelID,
			Cursor:           input.Cursor,
			IncludeBots:      input.IncludeBots,
			IncludeContext:   input.IncludeContext,
			Limit:            input.Limit,
			MaxContextLength: input.MaxContextLength,
			Query:            input.Query,
			ResponseFormat:   input.ResponseFormat,
			Sort:             input.Sort,
			SortDir:          input.SortDir,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_search_public_and_private",
		Description: "Search public and private Slack messages and files. Same as slack_search_public but can include private channels, group DMs, and IMs. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		After            string `json:"after,omitempty" jsonschema:"Unix timestamp lower bound"`
		Before           string `json:"before,omitempty" jsonschema:"Unix timestamp upper bound"`
		ChannelTypes     string `json:"channel_types,omitempty" jsonschema:"Comma-separated: public_channel, private_channel, mpim, im (default all)"`
		ContentTypes     string `json:"content_types,omitempty" jsonschema:"Comma-separated: messages, files"`
		ContextChannelID string `json:"context_channel_id,omitempty" jsonschema:"Boost results for this channel"`
		Cursor           string `json:"cursor,omitempty" jsonschema:"Pagination cursor"`
		IncludeBots      bool   `json:"include_bots,omitempty" jsonschema:"Include bot messages"`
		IncludeContext   *bool  `json:"include_context,omitempty" jsonschema:"Include surrounding context (default true)"`
		Limit            int    `json:"limit,omitempty" jsonschema:"Results, max 20 (default 20)"`
		MaxContextLength int    `json:"max_context_length,omitempty" jsonschema:"Max chars per context message"`
		Query            string `json:"query" jsonschema:"Search query with modifiers"`
		ResponseFormat   string `json:"response_format,omitempty" jsonschema:"detailed or concise (default detailed)"`
		Sort             string `json:"sort,omitempty" jsonschema:"score or timestamp (default score)"`
		SortDir          string `json:"sort_dir,omitempty" jsonschema:"asc or desc (default desc)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Query == "" {
			return errResult("query is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.SearchPublicAndPrivate(ctx, SearchPublicAndPrivateIn{
			SearchPublicIn: SearchPublicIn{
				After:            input.After,
				Before:           input.Before,
				ContentTypes:     input.ContentTypes,
				ContextChannelID: input.ContextChannelID,
				Cursor:           input.Cursor,
				IncludeBots:      input.IncludeBots,
				IncludeContext:   input.IncludeContext,
				Limit:            input.Limit,
				MaxContextLength: input.MaxContextLength,
				Query:            input.Query,
				ResponseFormat:   input.ResponseFormat,
				Sort:             input.Sort,
				SortDir:          input.SortDir,
			},
			ChannelTypes: input.ChannelTypes,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_send_message",
		Description: "Send a message to a Slack channel or DM. Supports thread replies and broadcast. Requires Slack write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ChannelID      string `json:"channel" jsonschema:"Channel or user ID (for DMs)"`
		DraftID        string `json:"draft_id,omitempty" jsonschema:"Draft ID to delete after sending"`
		Message        string `json:"message" jsonschema:"Markdown-formatted message (max 5000 chars)"`
		ReplyBroadcast bool   `json:"reply_broadcast,omitempty" jsonschema:"Also post reply to channel"`
		ThreadTS       string `json:"thread_ts,omitempty" jsonschema:"Parent message timestamp (for thread replies)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.ChannelID == "" {
			return errResult("channel is required")
		}
		if input.Message == "" {
			return errResult("message is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "write")
		if err != nil {
			return errResult(err.Error())
		}
		msg := input.Message + brandingFooter(ctx, req, branding, baseURL, "Sent", "slack")
		out, err := client.SendMessage(ctx, SlackSendMessageIn{
			ChannelID:      input.ChannelID,
			DraftID:        input.DraftID,
			Message:        msg,
			ReplyBroadcast: input.ReplyBroadcast,
			ThreadTS:       input.ThreadTS,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_send_message_draft",
		Description: "Create a draft message as a DM to yourself for review. Returns a draft_id that can be passed to slack_send_message to clean up after sending. Requires Slack draft connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ChannelID string `json:"channel" jsonschema:"Channel to create draft for"`
		Message   string `json:"message" jsonschema:"Message content (Slack mrkdwn)"`
		ThreadTS  string `json:"thread_ts,omitempty" jsonschema:"Parent message timestamp (for thread reply draft)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.ChannelID == "" {
			return errResult("channel is required")
		}
		if input.Message == "" {
			return errResult("message is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		draftMsg := input.Message + brandingFooter(ctx, req, branding, baseURL, "Sent", "slack")
		out, err := client.SendMessageDraft(ctx, SlackSendMessageDraftIn{
			ChannelID: input.ChannelID,
			Message:   draftMsg,
			ThreadTS:  input.ThreadTS,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_schedule_message",
		Description: "Schedule a message for future delivery. Requires Slack write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ChannelID      string `json:"channel" jsonschema:"Target channel"`
		Message        string `json:"message" jsonschema:"Message content"`
		PostAt         int    `json:"post_at" jsonschema:"Unix timestamp (min 2 min future, max 120 days)"`
		ReplyBroadcast bool   `json:"reply_broadcast,omitempty" jsonschema:"Broadcast thread reply to channel"`
		ThreadTS       string `json:"thread_ts,omitempty" jsonschema:"Parent message timestamp (for thread reply)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.ChannelID == "" {
			return errResult("channel is required")
		}
		if input.Message == "" {
			return errResult("message is required")
		}
		if input.PostAt == 0 {
			return errResult("post_at is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "write")
		if err != nil {
			return errResult(err.Error())
		}
		schedMsg := input.Message + brandingFooter(ctx, req, branding, baseURL, "Sent", "slack")
		out, err := client.ScheduleMessage(ctx, SlackScheduleMessageIn{
			ChannelID:      input.ChannelID,
			Message:        schedMsg,
			PostAt:         input.PostAt,
			ReplyBroadcast: input.ReplyBroadcast,
			ThreadTS:       input.ThreadTS,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_create_canvas",
		Description: "Create a new Slack canvas with markdown content. Requires Slack draft or write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Content string `json:"content" jsonschema:"Markdown content (headers max depth 3, full HTTP links only)"`
		Title   string `json:"title" jsonschema:"Canvas title"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Title == "" {
			return errResult("title is required")
		}
		if input.Content == "" {
			return errResult("content is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		canvasContent := input.Content + brandingFooter(ctx, req, branding, baseURL, "Created", "slack")
		out, err := client.CreateCanvas(ctx, CreateCanvasIn{
			Content: canvasContent,
			Title:   input.Title,
		})
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	return server
}
