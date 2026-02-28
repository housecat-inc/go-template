package srv

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"srv.housecat.com/ui/layouts"
	"srv.housecat.com/ui/pages"
)

const llmGatewayURL = "http://169.254.169.254/gateway/llm/anthropic/v1/messages"
const llmModel = "claude-sonnet-4-5-20250929"

type chatRequest struct {
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

func (s *Server) HandleChat(c echo.Context) error {
	r := c.Request()
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

	nav := layouts.NavData{
		Title:     s.Hostname,
		UserEmail: userEmail,
		LoginURL:  loginURLForRequest(r),
		LogoutURL: logoutURL,
	}
	component := pages.Chat(nav)
	return component.Render(r.Context(), c.Response())
}

func (s *Server) HandleChatAPI(c echo.Context) error {
	var req chatRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if len(req.Messages) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "no messages")
	}

	body := map[string]any{
		"max_tokens": 4096,
		"messages":   req.Messages,
		"model":      llmModel,
		"stream":     true,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "marshal error")
	}

	httpReq, err := http.NewRequestWithContext(c.Request().Context(), "POST", llmGatewayURL, bytes.NewReader(jsonBody))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "request error")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		slog.Error("llm gateway request", "error", err)
		return echo.NewHTTPError(http.StatusBadGateway, "gateway error")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		slog.Error("llm gateway error", "status", resp.StatusCode, "body", string(errBody))
		return echo.NewHTTPError(http.StatusBadGateway, "llm error: "+string(errBody))
	}

	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event struct {
			Delta struct {
				Text string `json:"text"`
				Type string `json:"type"`
			} `json:"delta"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event.Type != "content_block_delta" || event.Delta.Type != "text_delta" {
			continue
		}

		chunk, _ := json.Marshal(map[string]string{"text": event.Delta.Text})
		_, _ = w.Write([]byte("data: " + string(chunk) + "\n\n"))
		w.Flush()
	}

	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	w.Flush()
	return nil
}
