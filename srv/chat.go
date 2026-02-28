package srv

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"

	"srv.housecat.com/db/dbgen"
	"srv.housecat.com/ui/layouts"
	"srv.housecat.com/ui/pages"
)

const llmGatewayURL = "http://169.254.169.254/gateway/llm/anthropic/v1/messages"
const llmModel = "claude-sonnet-4-5-20250929"

type chatRequest struct {
	ChatID   string        `json:"chat_id"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

func (s *Server) HandleChat(c echo.Context) error {
	r := c.Request()
	userEmail := c.Get("userEmail").(string)
	userID := c.Get("userID").(string)
	logoutURL := c.Get("logoutURL").(string)
	chatID := c.QueryParam("id")

	nav := layouts.NavData{
		Title:     s.Hostname,
		UserEmail: userEmail,
		LoginURL:  loginURLForRequest(r),
		LogoutURL: logoutURL,
	}

	q := dbgen.New(s.DB)
	chats, err := q.ListChatsByUser(r.Context(), dbgen.ListChatsByUserParams{
		UserID: userID,
		Limit:  50,
	})
	if err != nil {
		slog.Error("list chats", "error", err)
	}

	var msgs []dbgen.Message
	if chatID != "" {
		msgs, err = q.ListMessagesByChat(r.Context(), chatID)
		if err != nil {
			slog.Error("list messages", "error", err)
		}
	}

	data := pages.ChatData{
		ChatID:   chatID,
		Chats:    chats,
		Messages: msgs,
		Nav:      nav,
	}

	component := pages.Chat(data)
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

	ctx := c.Request().Context()
	userID := c.Get("userID").(string)
	q := dbgen.New(s.DB)

	if req.ChatID == "" {
		id, err := randomHex(16)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "id generation")
		}
		req.ChatID = id
		title := req.Messages[0].Content
		if len(title) > 80 {
			title = title[:80] + "…"
		}
		if err := q.InsertChat(ctx, dbgen.InsertChatParams{
			ID:     req.ChatID,
			Title:  title,
			UserID: userID,
		}); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, errors.Wrap(err, "insert chat").Error())
		}
	}

	lastMsg := req.Messages[len(req.Messages)-1]
	if err := q.InsertMessage(ctx, dbgen.InsertMessageParams{
		ChatID:  req.ChatID,
		Content: lastMsg.Content,
		Role:    lastMsg.Role,
	}); err != nil {
		slog.Error("insert user message", "error", err)
	}

	_ = q.UpdateChatTimestamp(ctx, req.ChatID)

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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", llmGatewayURL, bytes.NewReader(jsonBody))
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

	meta, _ := json.Marshal(map[string]string{"chat_id": req.ChatID})
	_, _ = w.Write([]byte("data: " + string(meta) + "\n\n"))
	w.Flush()

	var fullText strings.Builder
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

		fullText.WriteString(event.Delta.Text)
		chunk, _ := json.Marshal(map[string]string{"text": event.Delta.Text})
		_, _ = w.Write([]byte("data: " + string(chunk) + "\n\n"))
		w.Flush()
	}

	if fullText.Len() > 0 {
		if err := q.InsertMessage(ctx, dbgen.InsertMessageParams{
			ChatID:  req.ChatID,
			Content: fullText.String(),
			Role:    "model",
		}); err != nil {
			slog.Error("insert model message", "error", err)
		}
	}

	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	w.Flush()
	return nil
}
