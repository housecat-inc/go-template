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

var services = []ServiceStatus{
	{
		Name:        "gcal",
		DisplayName: "Google Calendar",
		Description: "Calendar access via Google Calendar API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Read events", Scopes: []string{"https://www.googleapis.com/auth/calendar.readonly"}},
			{Level: "write", DisplayName: "Manage events", Scopes: []string{"https://www.googleapis.com/auth/calendar"}},
		},
	},
	{
		Name:        "gdrive",
		DisplayName: "Google Drive",
		Description: "File access via Google Drive API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Read files", Scopes: []string{"https://www.googleapis.com/auth/drive.readonly"}},
			{Level: "write", DisplayName: "Manage files", Scopes: []string{"https://www.googleapis.com/auth/drive"}},
		},
	},
	{
		Name:        "gmail",
		DisplayName: "Gmail",
		Description: "Email access via Gmail API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Read emails", Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}},
			{Level: "draft", DisplayName: "Draft emails", Scopes: []string{"https://www.googleapis.com/auth/gmail.compose"}},
			{Level: "write", DisplayName: "Send emails", Scopes: []string{"https://www.googleapis.com/auth/gmail.send"}},
		},
	},
	{
		Name:        "granola",
		DisplayName: "Granola",
		Description: "Meeting notes via Granola API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Read notes", Scopes: []string{"granola:read"}},
		},
	},
	{
		Name:        "notion",
		DisplayName: "Notion",
		Description: "Pages and databases via Notion API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Read pages", Scopes: []string{"notion:read"}},
			{Level: "write", DisplayName: "Manage pages", Scopes: []string{"notion:write"}},
		},
	},
	{
		Name:        "slack",
		DisplayName: "Slack",
		Description: "Messaging via Slack API",
		Levels: []AccessLevel{
			{Level: "read", DisplayName: "Read messages", Scopes: []string{"channels:history", "channels:read"}},
			{Level: "write", DisplayName: "Send messages", Scopes: []string{"chat:write", "channels:history", "channels:read"}},
		},
	},
}

func connections(ctx context.Context, req *gomcp.CallToolRequest, input struct{}) (*gomcp.CallToolResult, any, error) {
	data, err := json.Marshal(services)
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


