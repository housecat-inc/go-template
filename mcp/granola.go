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
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
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

	if rpcResp.Result.IsError {
		var texts []string
		for _, c := range rpcResp.Result.Content {
			if c.Type == "text" {
				texts = append(texts, c.Text)
			}
		}
		return nil, errors.Newf("granola tool error: %s", strings.Join(texts, "; "))
	}

	var texts []string
	for _, c := range rpcResp.Result.Content {
		if c.Type == "text" {
			texts = append(texts, c.Text)
		}
	}

	result := strings.Join(texts, "\n")
	return json.RawMessage(result), nil
}
