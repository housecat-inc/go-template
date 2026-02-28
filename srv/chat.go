package srv

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"
	"github.com/starfederation/datastar-go/datastar"

	"srv.housecat.com/db/dbgen"
	"srv.housecat.com/ui/layouts"
	"srv.housecat.com/ui/pages"
)

const llmGatewayURL = "http://169.254.169.254/gateway/llm/anthropic/v1/messages"
const llmModel = "claude-sonnet-4-5-20250929"

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

type chatSignals struct {
	ChatID   string        `json:"chatId"`
	Input    string        `json:"input"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

func (s *Server) HandleChatSend(c echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	userID := c.Get("userID").(string)

	var signals chatSignals
	if err := datastar.ReadSignals(r, &signals); err != nil {
		slog.Error("read signals", "error", err)
		return echo.NewHTTPError(http.StatusBadRequest, "invalid signals")
	}

	if strings.TrimSpace(signals.Input) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "empty message")
	}

	q := dbgen.New(s.DB)

	newChat := signals.ChatID == ""
	if newChat {
		id, err := randomHex(16)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "id generation")
		}
		signals.ChatID = id
		title := signals.Input
		if len(title) > 80 {
			title = title[:80] + "…"
		}
		if err := q.InsertChat(ctx, dbgen.InsertChatParams{
			ID:     signals.ChatID,
			Title:  title,
			UserID: userID,
		}); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, errors.Wrap(err, "insert chat").Error())
		}
	}

	sse := datastar.NewSSE(c.Response(), r)

	if newChat {
		sse.ExecuteScript(fmt.Sprintf(`window.history.replaceState({}, "", "/chat?id=%s")`, signals.ChatID))
	}

	signals.Messages = append(signals.Messages, chatMessage{
		Content: signals.Input,
		Role:    "user",
	})

	if err := q.InsertMessage(ctx, dbgen.InsertMessageParams{
		ChatID:  signals.ChatID,
		Content: signals.Input,
		Role:    "user",
	}); err != nil {
		slog.Error("insert user message", "error", err)
	}

	_ = q.UpdateChatTimestamp(ctx, signals.ChatID)

	isNewConversation := len(signals.Messages) == 1
	if isNewConversation {
		sse.PatchElementTempl(
			pages.ChatMainArea(pages.ChatData{ChatID: signals.ChatID, Messages: []dbgen.Message{{Content: signals.Input, Role: "user"}}}),
		)
	} else {
		sse.PatchElementTempl(
			pages.ChatBubble(dbgen.Message{Content: signals.Input, Role: "user"}),
			datastar.WithSelectorID("messages"),
			datastar.WithModeAppend(),
		)
	}

	sse.MarshalAndPatchSignals(map[string]any{
		"input":  "",
		"chatId": signals.ChatID,
	})

	sse.ExecuteScript("document.getElementById('messages').scrollTop = document.getElementById('messages').scrollHeight")

	llmMessages := make([]map[string]string, len(signals.Messages))
	for i, m := range signals.Messages {
		role := m.Role
		if role == "model" {
			role = "assistant"
		}
		llmMessages[i] = map[string]string{"role": role, "content": m.Content}
	}

	body := map[string]any{
		"max_tokens": 4096,
		"messages":   llmMessages,
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
		sse.PatchElements(
			`<div id="streaming-bubble" class="flex justify-start"><div class="max-w-[80%] rounded-lg px-4 py-2 text-sm whitespace-pre-wrap bg-red-100 dark:bg-red-900 text-foreground">Error connecting to LLM</div></div>`,
			datastar.WithSelectorID("messages"),
			datastar.WithModeAppend(),
		)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		slog.Error("llm gateway error", "status", resp.StatusCode, "body", string(errBody))
		sse.PatchElements(
			`<div id="streaming-bubble" class="flex justify-start"><div class="max-w-[80%] rounded-lg px-4 py-2 text-sm whitespace-pre-wrap bg-red-100 dark:bg-red-900 text-foreground">LLM error</div></div>`,
			datastar.WithSelectorID("messages"),
			datastar.WithModeAppend(),
		)
		return nil
	}

	sse.PatchElements(
		`<div id="streaming-bubble" class="flex justify-start"><div id="streaming-text" class="max-w-[80%] rounded-lg px-4 py-2 text-sm whitespace-pre-wrap bg-muted text-foreground"></div></div>`,
		datastar.WithSelectorID("messages"),
		datastar.WithModeAppend(),
	)

	var fullText strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if sse.IsClosed() {
			break
		}
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

		sse.PatchElements(
			fmt.Sprintf(`<div id="streaming-text" class="max-w-[80%%] rounded-lg px-4 py-2 text-sm whitespace-pre-wrap bg-muted text-foreground">%s</div>`, escapeHTML(fullText.String())),
		)

		sse.ExecuteScript("document.getElementById('messages').scrollTop = document.getElementById('messages').scrollHeight")
	}

	if fullText.Len() > 0 {
		if err := q.InsertMessage(ctx, dbgen.InsertMessageParams{
			ChatID:  signals.ChatID,
			Content: fullText.String(),
			Role:    "model",
		}); err != nil {
			slog.Error("insert model message", "error", err)
		}

		signals.Messages = append(signals.Messages, chatMessage{
			Content: fullText.String(),
			Role:    "model",
		})
		sse.MarshalAndPatchSignals(map[string]any{
			"messages":  signals.Messages,
			"streaming": false,
		})

		sse.PatchElementTempl(
			pages.ChatBubble(dbgen.Message{Content: fullText.String(), Role: "model"}),
			datastar.WithSelectorID("streaming-bubble"),
			datastar.WithModeReplace(),
		)
	}

	chats, err := q.ListChatsByUser(ctx, dbgen.ListChatsByUserParams{
		UserID: userID,
		Limit:  50,
	})
	if err == nil {
		sse.PatchElementTempl(
			pages.ChatSidebarContent(pages.ChatData{ChatID: signals.ChatID, Chats: chats}),
		)
	}

	return nil
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
