package mcp

import (
	"context"
	"encoding/json"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func stubLookup(tokens map[string]string) TokenLookup {
	return func(ctx context.Context, subject, service, level string) (string, error) {
		key := subject + ":" + service + ":" + level
		if tok, ok := tokens[key]; ok {
			return tok, nil
		}
		return "", ErrTokenNotFound
	}
}

func TestConnections(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer("https://example.com", stubLookup(nil), nil, nil, nil)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	a.NoError(err)
	a.GreaterOrEqual(len(tools.Tools), 25)

	toolNames := make([]string, len(tools.Tools))
	for i, t := range tools.Tools {
		toolNames[i] = t.Name
	}
	a.Contains(toolNames, "connections")
	a.Contains(toolNames, "gcal_create_event")
	a.Contains(toolNames, "gcal_get_event")
	a.Contains(toolNames, "gcal_list_calendars")
	a.Contains(toolNames, "gcal_list_events")
	a.Contains(toolNames, "gcal_quick_add")
	a.Contains(toolNames, "gdrive_add_permission")
	a.Contains(toolNames, "gdrive_create_file")
	a.Contains(toolNames, "gdrive_get_file")
	a.Contains(toolNames, "gdrive_list_files")
	a.Contains(toolNames, "gdrive_list_permissions")
	a.Contains(toolNames, "gdrive_read_file")
	a.Contains(toolNames, "gmail_create_draft")
	a.Contains(toolNames, "gmail_get_profile")
	a.Contains(toolNames, "gmail_list_drafts")
	a.Contains(toolNames, "gmail_list_labels")
	a.Contains(toolNames, "gmail_read_message")
	a.Contains(toolNames, "gmail_read_thread")
	a.Contains(toolNames, "gmail_search_messages")
	a.Contains(toolNames, "gmail_send_message")
	// attio, granola and notion tools are registered dynamically from upstream MCP
	// and won't appear without upstreamTools passed to NewServer
	a.Contains(toolNames, "slack_create_canvas")
	a.Contains(toolNames, "slack_read_canvas")
	a.Contains(toolNames, "slack_read_channel")
	a.Contains(toolNames, "slack_read_thread")
	a.Contains(toolNames, "slack_read_user_profile")
	a.Contains(toolNames, "slack_schedule_message")
	a.Contains(toolNames, "slack_search_channels")
	a.Contains(toolNames, "slack_search_public")
	a.Contains(toolNames, "slack_search_public_and_private")
	a.Contains(toolNames, "slack_search_users")
	a.Contains(toolNames, "slack_send_message")
	a.Contains(toolNames, "slack_send_message_draft")

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "connections"})
	a.NoError(err)
	a.Len(res.Content, 1)

	text := res.Content[0].(*gomcp.TextContent).Text
	var resp ConnectionsResponse
	a.NoError(json.Unmarshal([]byte(text), &resp))
	a.Len(resp.Services, 9)
	a.Nil(resp.User)

	ids := make([]string, len(resp.Services))
	for i, s := range resp.Services {
		ids[i] = s.ID
		a.NotEmpty(s.Connections)
		for _, conn := range s.Connections {
			a.False(conn.Enabled)
			a.Equal("https://example.com/connect/"+s.ID+"/enable/"+conn.Level, conn.URL)
		}
	}
	a.Equal([]string{"attio", "gcal", "gdrive", "gdocs", "gmail", "gsheets", "granola", "notion", "slack"}, ids)
}

func TestGmailToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer("https://example.com", stubLookup(nil), nil, nil, nil)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "gmail_search_messages",
		Arguments: map[string]any{},
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}

func TestGCalToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer("https://example.com", stubLookup(nil), nil, nil, nil)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "gcal_list_calendars",
		Arguments: map[string]any{},
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}

func TestGDriveToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer("https://example.com", stubLookup(nil), nil, nil, nil)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "gdrive_list_files",
		Arguments: map[string]any{},
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}

func TestAttioToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	upstreamTools := []UpstreamTool{
		{Service: "attio", Name: "search-records", Description: "Search records", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
	}
	server := NewServer("https://example.com", stubLookup(nil), nil, nil, upstreamTools)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name: "search-records",
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}

func TestGranolaToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	upstreamTools := []UpstreamTool{
		{Service: "granola", Name: "list_meetings", Description: "List meetings", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
	}
	server := NewServer("https://example.com", stubLookup(nil), nil, nil, upstreamTools)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name: "list_meetings",
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}

func TestNotionToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	upstreamTools := []UpstreamTool{
		{Service: "notion", Name: "notion-search", Description: "Search Notion", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
	}
	server := NewServer("https://example.com", stubLookup(nil), nil, nil, upstreamTools)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name: "notion-search",
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}

func TestSlackToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer("https://example.com", stubLookup(nil), nil, nil, nil)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "slack_read_channel",
		Arguments: map[string]any{"channel": "C0123"},
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}
