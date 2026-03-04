package mcp

import (
	"context"
	"encoding/json"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func TestConnections(t *testing.T) {
	a := assert.New(t)
	ctx := context.Background()

	server := NewServer()
	clientTransport, serverTransport := gomcp.NewInMemoryTransports()

	_, err := server.Connect(ctx, serverTransport, nil)
	a.NoError(err)

	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	a.NoError(err)
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	a.NoError(err)
	a.Len(tools.Tools, 1)
	a.Equal("connections", tools.Tools[0].Name)

	res, err := session.CallTool(ctx, &gomcp.CallToolParams{Name: "connections"})
	a.NoError(err)
	a.Len(res.Content, 1)

	text := res.Content[0].(*gomcp.TextContent).Text
	var statuses []ServiceStatus
	a.NoError(json.Unmarshal([]byte(text), &statuses))
	a.Len(statuses, 6)

	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = s.Name
		a.Empty(s.CurrentLevel)
		a.NotEmpty(s.Levels)
	}
	a.Equal([]string{"gcal", "gdrive", "gmail", "granola", "notion", "slack"}, names)
}
