package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotionCallToolSSEResponse(t *testing.T) {
	a := assert.New(t)

	sseData := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hello world\"}]}}\n"

	parsed := sseData
	if strings.HasPrefix(parsed, "event:") {
		for _, line := range strings.Split(parsed, "\n") {
			if strings.HasPrefix(line, "data: ") {
				parsed = strings.TrimPrefix(line, "data: ")
				break
			}
		}
	}

	var rpcResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	err := json.Unmarshal([]byte(parsed), &rpcResp)
	a.NoError(err)
	a.Len(rpcResp.Result.Content, 1)
	a.Equal("hello world", rpcResp.Result.Content[0].Text)
}

func TestDefaultUpstreamTools(t *testing.T) {
	a := assert.New(t)

	tools := DefaultUpstreamTools()
	a.GreaterOrEqual(len(tools), 16, "should have at least 16 embedded tools")

	services := map[string]int{}
	for _, t := range tools {
		services[t.Service]++
		a.NotEmpty(t.Name)
		a.NotEmpty(t.Description)
		a.NotEmpty(t.InputSchema)
	}
	a.Equal(12, services["notion"])
	a.Equal(4, services["granola"])
}

func TestUpstreamToolsRegistered(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	ctx := context.Background()
	upstreamTools := []UpstreamTool{
		{Service: "granola", Name: "list_meetings", Description: "List meetings", InputSchema: json.RawMessage(`{"type":"object","properties":{}}`)},
		{Service: "notion", Name: "notion-search", Description: "Search Notion", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)},
	}
	server := NewServer("https://example.com", stubLookup(nil), nil, upstreamTools)

	clientTransport, serverTransport := gomcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err)
	defer serverSession.Close()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	r.NoError(err)
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	r.NoError(err)

	toolMap := map[string]bool{}
	for _, tool := range tools.Tools {
		toolMap[tool.Name] = true
	}

	a.True(toolMap["list_meetings"], "granola list_meetings should be registered")
	a.True(toolMap["notion-search"], "notion-search should be registered")
}
