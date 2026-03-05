package mcp

import (
	"context"
	"encoding/json"

	"github.com/cockroachdb/errors"
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
		ID:          "gmail",
		Name:        "Google Mail",
		Description: "Email access via Gmail API",
		Connections: []Connection{
			{Level: "read", Description: "Get emails and threads", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}},
			{Level: "draft", Description: "Create draft emails for review", Scopes: []string{"https://www.googleapis.com/auth/gmail.compose"}},
			{Level: "write", Description: "Send emails on your behalf", Scopes: []string{"https://www.googleapis.com/auth/gmail.send"}},
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
		Description: "Pages and databases via Notion API",
		Connections: []Connection{
			{Level: "read", Description: "Get pages and databases", Scopes: []string{"notion:read"}},
			{Level: "draft", Description: "Create private pages for review", Scopes: []string{"notion:write"}},
			{Level: "write", Description: "Create and share pages", Scopes: []string{"notion:write"}},
		},
	},
	{
		ID:          "slack",
		Name:        "Slack",
		Description: "Messaging via Slack API",
		Connections: []Connection{
			{Level: "read", Description: "Get messages and channels", Scopes: []string{"channels:history", "channels:read", "groups:history", "groups:read", "search:read", "users:read"}},
			{Level: "draft", Description: "Create DMs to self for review", Scopes: []string{"channels:history", "channels:read", "chat:write", "groups:history", "groups:read", "im:write", "search:read", "users:read"}},
			{Level: "write", Description: "Send messages to channels", Scopes: []string{"channels:history", "channels:read", "chat:write", "groups:history", "groups:read", "im:write", "search:read", "users:read"}},
		},
	},
}

type ConnectionsLookup func(ctx context.Context, userID string) map[string]map[string]bool

type TokenLookup func(ctx context.Context, userID, service, level string) (string, error)

func userIDFromRequest(req *gomcp.CallToolRequest) string {
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

func slackClientFromRequest(ctx context.Context, req *gomcp.CallToolRequest, lookup TokenLookup, minLevel string) (*SlackClient, error) {
	userID := userIDFromRequest(req)
	if userID == "" {
		return nil, errors.New("not authenticated")
	}

	levels := []string{minLevel}
	if minLevel == "read" {
		levels = []string{"read", "draft", "write"}
	} else if minLevel == "draft" {
		levels = []string{"draft", "write"}
	}

	for _, level := range levels {
		token, err := lookup(ctx, userID, "slack", level)
		if err != nil {
			if errors.Is(err, ErrTokenNotFound) {
				continue
			}
			return nil, errors.Wrap(err, "token lookup")
		}
		if token != "" {
			return &SlackClient{Token: token}, nil
		}
	}
	return nil, errors.Newf("slack not connected — connect at %s level or higher via the connections page", minLevel)
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
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{
			&gomcp.TextContent{Text: msg},
		},
		IsError: true,
	}, nil, nil
}

func NewServer(baseURL string, lookup TokenLookup, connLookup ConnectionsLookup) *gomcp.Server {
	server := gomcp.NewServer(&gomcp.Implementation{
		Name:    "housecat",
		Version: "0.1.0",
	}, nil)

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "connections",
		Description: "List integration connection statuses for Gmail, Google Calendar, Google Drive, Slack, Granola, and Notion",
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

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_list_channels",
		Description: "List Slack channels the user can access. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Cursor string `json:"cursor,omitempty" jsonschema:"Pagination cursor from previous response"`
		Limit  int    `json:"limit,omitempty" jsonschema:"Max channels to return (default 100)"`
	}) (*gomcp.CallToolResult, any, error) {
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.ListChannels(ctx, input.Cursor, input.Limit)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_get_channel_history",
		Description: "Get recent messages from a Slack channel. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ChannelID string `json:"channel_id" jsonschema:"Slack channel ID (e.g. C01234567)"`
		Limit     int    `json:"limit,omitempty" jsonschema:"Max messages to return (default 20)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.ChannelID == "" {
			return errResult("channel_id is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetChannelHistory(ctx, input.ChannelID, input.Limit)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_draft_message",
		Description: "Draft a message by sending it as a DM to yourself for review. Copy and paste to a channel when ready. Requires Slack draft connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Text string `json:"text" jsonschema:"Message text to draft"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Text == "" {
			return errResult("text is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "draft")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.DraftMessage(ctx, input.Text)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_post_message",
		Description: "Send a message to a Slack channel. Requires Slack write connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		ChannelID string `json:"channel_id" jsonschema:"Slack channel ID to post to"`
		Text      string `json:"text" jsonschema:"Message text to send"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.ChannelID == "" {
			return errResult("channel_id is required")
		}
		if input.Text == "" {
			return errResult("text is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "write")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.PostMessage(ctx, input.ChannelID, input.Text)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_search_messages",
		Description: "Search for messages across Slack channels. Requires Slack read connection. Uses Slack search syntax (e.g. 'in:#general from:@user keyword').",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		Count int    `json:"count,omitempty" jsonschema:"Max results to return (default 20)"`
		Query string `json:"query" jsonschema:"Search query using Slack search syntax"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.Query == "" {
			return errResult("query is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.SearchMessages(ctx, input.Query, input.Count)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "slack_get_user_profile",
		Description: "Get a Slack user's profile by their user ID. Requires Slack read connection.",
	}, func(ctx context.Context, req *gomcp.CallToolRequest, input struct {
		UserID string `json:"user_id" jsonschema:"Slack user ID (e.g. U01234567)"`
	}) (*gomcp.CallToolResult, any, error) {
		if input.UserID == "" {
			return errResult("user_id is required")
		}
		client, err := slackClientFromRequest(ctx, req, lookup, "read")
		if err != nil {
			return errResult(err.Error())
		}
		out, err := client.GetUserProfile(ctx, input.UserID)
		if err != nil {
			return errResult(err.Error())
		}
		result, err := textResult(out)
		return result, nil, err
	})

	return server
}
