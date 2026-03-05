package mcp

import (
	"context"
	"encoding/json"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func stubLookup(tokens map[string]string) TokenLookup {
	return func(ctx context.Context, userID, service, level string) (string, error) {
		key := userID + ":" + service + ":" + level
		if tok, ok := tokens[key]; ok {
			return tok, nil
		}
		return "", ErrTokenNotFound
	}
}

func TestConnections(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer("https://example.com", stubLookup(nil), nil)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	a.NoError(err)
	a.GreaterOrEqual(len(tools.Tools), 13)

	toolNames := make([]string, len(tools.Tools))
	for i, t := range tools.Tools {
		toolNames[i] = t.Name
	}
	a.Contains(toolNames, "connections")
	a.Contains(toolNames, "gmail_create_draft")
	a.Contains(toolNames, "gmail_get_thread")
	a.Contains(toolNames, "gmail_list_labels")
	a.Contains(toolNames, "gmail_list_messages")
	a.Contains(toolNames, "gmail_read_message")
	a.Contains(toolNames, "gmail_send_message")
	a.Contains(toolNames, "slack_draft_message")
	a.Contains(toolNames, "slack_get_channel_history")
	a.Contains(toolNames, "slack_list_channels")
	a.Contains(toolNames, "slack_post_message")
	a.Contains(toolNames, "slack_search_messages")
	a.Contains(toolNames, "slack_get_user_profile")

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "connections"})
	a.NoError(err)
	a.Len(res.Content, 1)

	text := res.Content[0].(*gomcp.TextContent).Text
	var resp ConnectionsResponse
	a.NoError(json.Unmarshal([]byte(text), &resp))
	a.Len(resp.Services, 6)
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
	a.Equal([]string{"gcal", "gdrive", "gmail", "granola", "notion", "slack"}, ids)
}

func TestGmailToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer("https://example.com", stubLookup(nil), nil)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "gmail_list_messages",
		Arguments: map[string]any{},
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}

func TestSlackToolsRequireAuth(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer("https://example.com", stubLookup(nil), nil)
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{
		Name:      "slack_list_channels",
		Arguments: map[string]any{},
	})
	a.NoError(err)
	a.True(res.IsError)
	a.Contains(res.Content[0].(*gomcp.TextContent).Text, "not authenticated")
}
