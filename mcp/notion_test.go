package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePageWithProperties(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		json.Unmarshal(body, &capturedBody)

		if req.URL.Path == "/v1/databases/db-123" {
			w.Write([]byte(`{"properties":{"Name":{"type":"title"}}}`))
			return
		}

		w.Write([]byte(`{"id":"page-123","created_time":"2026-01-01","properties":{}}`))
	}))
	defer ts.Close()

	client := &NotionClient{Token: "test-token", BaseURL: ts.URL + "/v1"}

	props := json.RawMessage(`{
		"Granola ID": {"rich_text": [{"text": {"content": "g-123"}}]},
		"Date": {"date": {"start": "2026-03-09"}},
		"Type": {"multi_select": [{"name": "Meeting Notes"}]}
	}`)

	out, err := client.CreatePage(context.Background(), "", "db-123", "Test Page", "", props)
	r.NoError(err)
	a.Equal("page-123", out.ID)

	payloadProps, ok := capturedBody["properties"].(map[string]any)
	r.True(ok, "properties should be a map")

	a.Contains(payloadProps, "Granola ID")
	a.Contains(payloadProps, "Date")
	a.Contains(payloadProps, "Type")
	a.Contains(payloadProps, "Name")
}

func TestCreatePageWithoutExtraProperties(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	var capturedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		json.Unmarshal(body, &capturedBody)

		if req.URL.Path == "/v1/databases/db-123" {
			w.Write([]byte(`{"properties":{"Name":{"type":"title"}}}`))
			return
		}

		w.Write([]byte(`{"id":"page-456","created_time":"2026-01-01","properties":{}}`))
	}))
	defer ts.Close()

	client := &NotionClient{Token: "test-token", BaseURL: ts.URL + "/v1"}
	out, err := client.CreatePage(context.Background(), "", "db-123", "Title Only", "", nil)
	r.NoError(err)
	a.Equal("page-456", out.ID)

	payloadProps, ok := capturedBody["properties"].(map[string]any)
	r.True(ok)
	a.Contains(payloadProps, "Name")
	a.Len(payloadProps, 1)
}

func TestUpdatePageProperties(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	var capturedBody map[string]any
	var capturedMethod string
	var capturedPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		capturedMethod = req.Method
		capturedPath = req.URL.Path
		body, _ := io.ReadAll(req.Body)
		json.Unmarshal(body, &capturedBody)
		w.Write([]byte(`{"id":"page-456","created_time":"2026-01-01","properties":{}}`))
	}))
	defer ts.Close()

	client := &NotionClient{Token: "test-token", BaseURL: ts.URL + "/v1"}

	props := json.RawMessage(`{
		"Granola ID": {"rich_text": [{"text": {"content": "updated-789"}}]},
		"Date": {"date": {"start": "2026-03-10"}}
	}`)

	out, err := client.UpdatePage(context.Background(), "page-456", props)
	r.NoError(err)
	a.Equal("page-456", out.ID)
	a.Equal("PATCH", capturedMethod)
	a.Equal("/v1/pages/page-456", capturedPath)

	payloadProps, ok := capturedBody["properties"].(map[string]any)
	r.True(ok, "properties should be a map")
	a.Contains(payloadProps, "Granola ID")
	a.Contains(payloadProps, "Date")
}

func TestGetDatabaseIncludesProperties(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`{
			"id": "db-789",
			"title": [{"plain_text": "Documents"}],
			"description": [],
			"properties": {
				"Name": {"id": "title", "type": "title", "title": {}},
				"Date": {"id": "abc", "type": "date", "date": {}},
				"Granola ID": {"id": "def", "type": "rich_text", "rich_text": {}}
			}
		}`))
	}))
	defer ts.Close()

	client := &NotionClient{Token: "test-token", BaseURL: ts.URL + "/v1"}
	out, err := client.GetDatabase(context.Background(), "db-789")
	r.NoError(err)
	a.Equal("db-789", out.ID)
	a.Equal("Documents", out.Title)

	var props map[string]any
	r.NoError(json.Unmarshal(out.Properties, &props))
	a.Contains(props, "Name")
	a.Contains(props, "Date")
	a.Contains(props, "Granola ID")
}

func TestNotionPropertiesSchemaIsObject(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	ctx := context.Background()
	server := NewServer("https://example.com", stubLookup(nil), nil)
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

	for _, tool := range tools.Tools {
		if tool.Name != "notion_update_page" && tool.Name != "notion_create_page" {
			continue
		}
		schemaJSON, _ := json.Marshal(tool.InputSchema)
		var schema map[string]any
		r.NoError(json.Unmarshal(schemaJSON, &schema))

		props, ok := schema["properties"].(map[string]any)
		r.True(ok, "%s schema properties should be a map", tool.Name)
		propSchema, ok := props["properties"].(map[string]any)
		r.True(ok, "%s properties.properties should be a map", tool.Name)
		a.Equal("object", propSchema["type"], "%s properties field should have type 'object', not array", tool.Name)
	}
}
