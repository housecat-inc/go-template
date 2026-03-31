package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
)

func fakeReq() *gomcp.CallToolRequest {
	return &gomcp.CallToolRequest{
		Extra: &gomcp.RequestExtra{
			TokenInfo: &auth.TokenInfo{UserID: "test-user"},
		},
	}
}

func notionLookup(connected map[string]bool) TokenLookup {
	return func(ctx context.Context, subject, service, level string) (string, error) {
		if service != "notion" {
			return "", ErrTokenNotFound
		}
		if connected[level] {
			return "tok-" + level, nil
		}
		return "", ErrTokenNotFound
	}
}

func TestNotionReadLevelGating(t *testing.T) {
	a := assert.New(t)

	t.Run("read token resolves for read tools", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"read": true})
		token, err := tokenForService(context.Background(), fakeReq(), lookup, "notion", "read")
		a.NoError(err)
		a.Equal("tok-read", token)
	})

	t.Run("draft token resolves for read tools via fallback", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"draft": true})
		token, err := tokenForService(context.Background(), fakeReq(), lookup, "notion", "read")
		a.NoError(err)
		a.Equal("tok-draft", token)
	})

	t.Run("write token resolves for read tools via fallback", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"write": true})
		token, err := tokenForService(context.Background(), fakeReq(), lookup, "notion", "read")
		a.NoError(err)
		a.Equal("tok-write", token)
	})

	t.Run("archive token does not resolve for read tools", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"archive": true})
		_, err := tokenForService(context.Background(), fakeReq(), lookup, "notion", "read")
		a.Error(err)
		a.Contains(err.Error(), "read level not connected")
	})
}

func TestNotionDraftLevelGating(t *testing.T) {
	a := assert.New(t)

	t.Run("draft token resolves for draft tools", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"draft": true})
		_, level, err := tokenForServiceWithLevel(context.Background(), fakeReq(), lookup, "notion", "draft")
		a.NoError(err)
		a.Equal("draft", level)
	})

	t.Run("write token resolves for draft tools", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"write": true})
		_, level, err := tokenForServiceWithLevel(context.Background(), fakeReq(), lookup, "notion", "draft")
		a.NoError(err)
		a.Equal("write", level)
	})

	t.Run("read token does not resolve for draft tools", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"read": true})
		_, _, err := tokenForServiceWithLevel(context.Background(), fakeReq(), lookup, "notion", "draft")
		a.Error(err)
		a.Contains(err.Error(), "draft level not connected")
	})
}

func TestNotionWriteLevelGating(t *testing.T) {
	a := assert.New(t)

	t.Run("write token resolves for write tools", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"write": true})
		token, err := tokenForService(context.Background(), fakeReq(), lookup, "notion", "write")
		a.NoError(err)
		a.Equal("tok-write", token)
	})

	t.Run("draft token does not resolve for write tools", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"draft": true})
		_, err := tokenForService(context.Background(), fakeReq(), lookup, "notion", "write")
		a.Error(err)
	})

	t.Run("read token does not resolve for write tools", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"read": true})
		_, err := tokenForService(context.Background(), fakeReq(), lookup, "notion", "write")
		a.Error(err)
	})
}

func TestNotionArchiveLevelGating(t *testing.T) {
	a := assert.New(t)

	t.Run("archive token resolves for delete", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"archive": true})
		token, level, err := tokenForServiceDeleteLevel(context.Background(), fakeReq(), lookup, "notion")
		a.NoError(err)
		a.Equal("tok-archive", token)
		a.Equal("archive", level)
	})

	t.Run("draft token resolves for delete", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"draft": true})
		token, level, err := tokenForServiceDeleteLevel(context.Background(), fakeReq(), lookup, "notion")
		a.NoError(err)
		a.Equal("tok-draft", token)
		a.Equal("draft", level)
	})

	t.Run("write token does not resolve for delete", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"write": true})
		_, _, err := tokenForServiceDeleteLevel(context.Background(), fakeReq(), lookup, "notion")
		a.Error(err)
		a.Contains(err.Error(), "archive or draft level required")
	})

	t.Run("read token does not resolve for delete", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"read": true})
		_, _, err := tokenForServiceDeleteLevel(context.Background(), fakeReq(), lookup, "notion")
		a.Error(err)
	})
}

func TestNotionDraftParentGating(t *testing.T) {
	a := assert.New(t)

	t.Run("draft level without parent is allowed", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"draft": true})
		_, level, err := tokenForServiceWithLevel(context.Background(), fakeReq(), lookup, "notion", "draft")
		a.NoError(err)
		a.Equal("draft", level)

		args := map[string]any{
			"pages": []any{map[string]any{"properties": map[string]any{"title": "Test"}}},
		}
		_, hasParent := args["parent"]
		a.False(hasParent)
	})

	t.Run("draft level with parent is rejected", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"draft": true})
		_, level, err := tokenForServiceWithLevel(context.Background(), fakeReq(), lookup, "notion", "draft")
		a.NoError(err)
		a.Equal("draft", level)

		args := map[string]any{
			"parent": map[string]any{"page_id": "abc123"},
			"pages":  []any{map[string]any{"properties": map[string]any{"title": "Test"}}},
		}
		_, hasParent := args["parent"]
		a.True(hasParent)
		a.True(level == "draft" && hasParent, "should reject draft with parent")
	})

	t.Run("write level with parent is allowed", func(t *testing.T) {
		lookup := notionLookup(map[string]bool{"write": true})
		_, level, err := tokenForServiceWithLevel(context.Background(), fakeReq(), lookup, "notion", "draft")
		a.NoError(err)
		a.Equal("write", level)

		args := map[string]any{
			"parent": map[string]any{"page_id": "abc123"},
			"pages":  []any{map[string]any{"properties": map[string]any{"title": "Test"}}},
		}
		_, hasParent := args["parent"]
		a.True(hasParent)
		a.False(level == "draft" && hasParent, "write level with parent should be allowed")
	})
}

