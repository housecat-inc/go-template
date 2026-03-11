package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"
)

const notionMCPEndpoint = "https://mcp.notion.com/mcp"

type NotionClient struct {
	Token string
}

// sessionCache caches MCP session IDs per access token to avoid re-initializing
// on every tool call. Sessions may expire upstream, in which case we retry once.
var (
	sessionCache   = map[string]string{}
	sessionCacheMu sync.Mutex
)

func getCachedSession(token string) string {
	sessionCacheMu.Lock()
	defer sessionCacheMu.Unlock()
	return sessionCache[token]
}

func setCachedSession(token, sessionID string) {
	sessionCacheMu.Lock()
	defer sessionCacheMu.Unlock()
	sessionCache[token] = sessionID
}

func clearCachedSession(token string) {
	sessionCacheMu.Lock()
	defer sessionCacheMu.Unlock()
	delete(sessionCache, token)
}

func (c *NotionClient) post(ctx context.Context, payload any, sessionID string) (*http.Response, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, errors.Wrap(err, "marshal request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, notionMCPEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.Wrap(err, "create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, errors.Wrap(err, "notion mcp request")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, errors.Wrap(err, "read response")
	}

	return resp, data, nil
}

func (c *NotionClient) initialize(ctx context.Context) (string, error) {
	reqID := mcpRequestID.Add(1)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":   map[string]any{},
			"clientInfo": map[string]any{
				"name":    "Housecat",
				"version": "1.0.0",
			},
		},
	}

	resp, data, err := c.post(ctx, payload, "")
	if err != nil {
		return "", errors.Wrap(err, "initialize")
	}

	if resp.StatusCode >= 400 {
		return "", errors.Newf("notion mcp initialize error (%d): %s", resp.StatusCode, string(data))
	}

	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		return "", errors.New("notion mcp: no session ID in initialize response")
	}

	notification := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	_, _, err = c.post(ctx, notification, sessionID)
	if err != nil {
		return "", errors.Wrap(err, "send initialized notification")
	}

	return sessionID, nil
}

// getSession returns a cached session or initializes a new one.
func (c *NotionClient) getSession(ctx context.Context) (string, error) {
	if sid := getCachedSession(c.Token); sid != "" {
		return sid, nil
	}
	sid, err := c.initialize(ctx)
	if err != nil {
		return "", err
	}
	setCachedSession(c.Token, sid)
	return sid, nil
}

// parseSSEData extracts the last JSON-RPC data payload from an SSE response.
// Scans all lines for "data:" prefixed content regardless of event framing.
func parseSSEData(raw string) string {
	var last string
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "data: ") {
			last = strings.TrimPrefix(line, "data: ")
		} else if strings.HasPrefix(line, "data:") {
			last = strings.TrimPrefix(line, "data:")
		}
	}
	if last != "" {
		return last
	}
	return raw
}

func (c *NotionClient) parseRPCResponse(data []byte) (json.RawMessage, error) {
	parsed := parseSSEData(string(data))

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(parsed), &rpcResp); err != nil {
		return nil, errors.Wrap(err, "decode mcp response")
	}

	if rpcResp.Error != nil {
		return nil, errors.Newf("notion mcp error: %s", rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

func (c *NotionClient) ListTools(ctx context.Context) (json.RawMessage, error) {
	sessionID, err := c.initialize(ctx)
	if err != nil {
		return nil, err
	}

	reqID := mcpRequestID.Add(1)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "tools/list",
		"params":  map[string]any{},
	}

	resp, data, err := c.post(ctx, payload, sessionID)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, errors.Newf("notion mcp error (%d): %s", resp.StatusCode, string(data))
	}

	result, err := c.parseRPCResponse(data)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *NotionClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (json.RawMessage, error) {
	return c.callToolWithRetry(ctx, toolName, arguments, true)
}

func (c *NotionClient) callToolWithRetry(ctx context.Context, toolName string, arguments map[string]any, canRetry bool) (json.RawMessage, error) {
	sessionID, err := c.getSession(ctx)
	if err != nil {
		return nil, err
	}

	reqID := mcpRequestID.Add(1)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": arguments,
		},
	}

	resp, data, err := c.post(ctx, payload, sessionID)
	if err != nil {
		return nil, err
	}

	// If session expired, clear cache and retry once with a fresh session.
	if resp.StatusCode == http.StatusBadRequest && canRetry {
		clearCachedSession(c.Token)
		return c.callToolWithRetry(ctx, toolName, arguments, false)
	}

	if resp.StatusCode >= 400 {
		return nil, errors.Newf("notion mcp error (%d): %s", resp.StatusCode, string(data))
	}

	result, err := c.parseRPCResponse(data)
	if err != nil {
		return nil, err
	}

	var toolResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return nil, errors.Wrap(err, "decode tool result")
	}

	if toolResult.IsError {
		var texts []string
		for _, c := range toolResult.Content {
			if c.Type == "text" {
				texts = append(texts, c.Text)
			}
		}
		return nil, errors.Newf("notion tool error: %s", strings.Join(texts, "; "))
	}

	var texts []string
	for _, c := range toolResult.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}

	return json.RawMessage(strings.Join(texts, "\n")), nil
}
