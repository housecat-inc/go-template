package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/cockroachdb/errors"
)

const granolaMCPEndpoint = "https://mcp.granola.ai/mcp"

type GranolaClient struct {
	Token string
}

var mcpRequestID atomic.Int64

func (c *GranolaClient) postRPC(ctx context.Context, payload any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, granolaMCPEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "granola mcp request")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}

	if resp.StatusCode >= 400 {
		return nil, errors.Newf("granola mcp error (%d): %s", resp.StatusCode, string(data))
	}

	parsed := string(data)
	if strings.HasPrefix(parsed, "event:") {
		lines := strings.Split(parsed, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.HasPrefix(lines[i], "data: ") {
				parsed = strings.TrimPrefix(lines[i], "data: ")
				break
			}
		}
	}

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
		return nil, errors.Newf("granola mcp error: %s", rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

func (c *GranolaClient) ListTools(ctx context.Context) (json.RawMessage, error) {
	reqID := mcpRequestID.Add(1)
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	return c.postRPC(ctx, payload)
}

func (c *GranolaClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (json.RawMessage, error) {
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

	result, err := c.postRPC(ctx, payload)
	if err != nil {
		return nil, err
	}

	// result is {"content": [...], "isError": bool}
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
		return nil, errors.Newf("granola tool error: %s", strings.Join(texts, "; "))
	}

	var texts []string
	for _, c := range toolResult.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}

	return json.RawMessage(strings.Join(texts, "\n")), nil
}
