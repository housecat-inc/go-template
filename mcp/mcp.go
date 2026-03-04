package mcp

import (
	"context"
	"encoding/json"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type AccessLevel struct {
	DisplayName string   `json:"displayName"`
	Level       string   `json:"level"`
	Scopes      []string `json:"scopes"`
}

type ServiceStatus struct {
	CurrentLevel string        `json:"currentLevel"`
	Description  string        `json:"description"`
	DisplayName  string        `json:"displayName"`
	Levels       []AccessLevel `json:"levels"`
	Name         string        `json:"name"`
}

type ConnectionsResponse struct {
	Services []ServiceStatus `json:"services"`
	User     *UserIdentity   `json:"user"`
}

type UserIdentity struct {
	Email string `json:"email"`
	ID    string `json:"id"`
}

var Services = []ServiceStatus{
	{
		Name:        "gcal",
		DisplayName: "Google Calendar",
		Description: "Calendar access via Google Calendar API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Get events", Scopes: []string{"https://www.googleapis.com/auth/calendar.readonly"}},
			{Level: "draft", DisplayName: "Create personal events for review", Scopes: []string{"https://www.googleapis.com/auth/calendar"}},
			{Level: "write", DisplayName: "Create events and invites", Scopes: []string{"https://www.googleapis.com/auth/calendar"}},
		},
	},
	{
		Name:        "gdrive",
		DisplayName: "Google Drive",
		Description: "File access via Google Drive API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Get files and folders", Scopes: []string{"https://www.googleapis.com/auth/drive.readonly"}},
			{Level: "draft", DisplayName: "Create private docs for review", Scopes: []string{"https://www.googleapis.com/auth/drive.file"}},
			{Level: "write", DisplayName: "Create and share files", Scopes: []string{"https://www.googleapis.com/auth/drive"}},
		},
	},
	{
		Name:        "gmail",
		DisplayName: "Google Mail",
		Description: "Email access via Gmail API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Get emails and threads", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}},
			{Level: "draft", DisplayName: "Create draft emails for review", Scopes: []string{"https://www.googleapis.com/auth/gmail.compose"}},
			{Level: "write", DisplayName: "Send emails on your behalf", Scopes: []string{"https://www.googleapis.com/auth/gmail.send"}},
		},
	},
	{
		Name:        "granola",
		DisplayName: "Granola",
		Description: "Meeting notes via Granola MCP",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Get meeting notes", Scopes: []string{"openid", "email", "offline_access"}},
		},
	},
	{
		Name:        "notion",
		DisplayName: "Notion",
		Description: "Pages and databases via Notion API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Get pages and databases", Scopes: []string{"notion:read"}},
			{Level: "draft", DisplayName: "Create private pages for review", Scopes: []string{"notion:write"}},
			{Level: "write", DisplayName: "Create and share pages", Scopes: []string{"notion:write"}},
		},
	},
	{
		Name:        "slack",
		DisplayName: "Slack",
		Description: "Messaging via Slack API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Get messages and channels", Scopes: []string{"channels:history", "channels:read"}},
			{Level: "draft", DisplayName: "Create DMs to self for review", Scopes: []string{"chat:write", "channels:history", "channels:read"}},
			{Level: "write", DisplayName: "Send messages to channels", Scopes: []string{"chat:write", "channels:history", "channels:read"}},
		},
	},
}

func connections(ctx context.Context, req *gomcp.CallToolRequest, input struct{}) (*gomcp.CallToolResult, any, error) {
	resp := ConnectionsResponse{Services: Services}

	if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
		resp.User = &UserIdentity{ID: extra.TokenInfo.UserID}
		if email, ok := extra.TokenInfo.Extra["email"].(string); ok {
			resp.User.Email = email
		}
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, nil, err
	}
	return &gomcp.CallToolResult{
		Content: []gomcp.Content{
			&gomcp.TextContent{Text: string(data)},
		},
	}, nil, nil
}

func NewServer() *gomcp.Server {
	server := gomcp.NewServer(&gomcp.Implementation{
		Name:    "housecat",
		Version: "0.1.0",
	}, nil)
	gomcp.AddTool(server, &gomcp.Tool{
		Name:        "connections",
		Description: "List integration connection statuses for Gmail, Google Calendar, Google Drive, Slack, Granola, and Notion",
	}, connections)
	return server
}


