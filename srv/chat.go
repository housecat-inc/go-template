package srv

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/labstack/echo/v4"

	"srv.housecat.com/db/dbgen"
	"srv.housecat.com/ui/pages"
)

func (s *Server) HandleChats(c echo.Context) error {
	r := c.Request()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

	q := dbgen.New(s.DB)
	chats, err := q.ListChatsByUser(r.Context(), userID)
	if err != nil {
		return errors.Wrap(err, "list chats")
	}

	var chatPreviews []pages.ChatPreview
	for _, ch := range chats {
		cp := pages.ChatPreview{
			ID:    ch.ID,
			Title: ch.Title,
		}
		msg, err := q.GetLatestMessage(r.Context(), ch.ID)
		if err == nil {
			cp.LastMessage = msg.Content
			cp.LastSender = emailToName(msg.SenderEmail)
			cp.LastTime = formatTime(msg.CreatedAt)
		}
		members, _ := q.ListChatMembers(r.Context(), ch.ID)
		cp.MemberCount = len(members)
		chatPreviews = append(chatPreviews, cp)
	}

	data := pages.ChatsPageData{
		Chats:     chatPreviews,
		LogoutURL: logoutURL,
		LoginURL:  loginURLForRequest(r),
		Hostname:  s.Hostname,
		UserEmail: userEmail,
	}

	return pages.ChatsPage(data).Render(r.Context(), c.Response())
}

func (s *Server) HandleChatView(c echo.Context) error {
	r := c.Request()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)
	logoutURL := c.Get("logoutURL").(string)

	chatID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid chat id")
	}

	q := dbgen.New(s.DB)

	isMember, err := q.IsChatMember(r.Context(), dbgen.IsChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil || !isMember {
		return echo.NewHTTPError(http.StatusForbidden, "not a member of this chat")
	}

	chat, err := q.GetChat(r.Context(), chatID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "chat not found")
	}

	messages, err := q.ListMessages(r.Context(), chatID)
	if err != nil {
		return errors.Wrap(err, "list messages")
	}

	members, _ := q.ListChatMembers(r.Context(), chatID)

	var msgViews []pages.MessageView
	for _, m := range messages {
		msgViews = append(msgViews, pages.MessageView{
			Content:     m.Content,
			ID:          m.ID,
			IsSent:      m.SenderID == userID,
			SenderEmail: m.SenderEmail,
			SenderName:  emailToName(m.SenderEmail),
			Time:        formatTime(m.CreatedAt),
		})
	}

	var memberViews []pages.MemberView
	for _, m := range members {
		memberViews = append(memberViews, pages.MemberView{
			Email: m.UserEmail,
			Name:  emailToName(m.UserEmail),
		})
	}

	data := pages.ChatViewData{
		ChatID:    chatID,
		Hostname:  s.Hostname,
		LoginURL:  loginURLForRequest(r),
		LogoutURL: logoutURL,
		Members:   memberViews,
		Messages:  msgViews,
		Title:     chat.Title,
		UserEmail: userEmail,
		UserID:    userID,
	}

	return pages.ChatView(data).Render(r.Context(), c.Response())
}

func (s *Server) HandleChatCreate(c echo.Context) error {
	r := c.Request()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)

	title := strings.TrimSpace(c.FormValue("title"))
	if title == "" {
		title = "New Chat"
	}

	q := dbgen.New(s.DB)
	chat, err := q.CreateChat(r.Context(), dbgen.CreateChatParams{
		OwnerID: userID,
		Title:   title,
	})
	if err != nil {
		return errors.Wrap(err, "create chat")
	}

	if err := q.AddChatMember(r.Context(), dbgen.AddChatMemberParams{
		ChatID:    chat.ID,
		UserEmail: userEmail,
		UserID:    userID,
	}); err != nil {
		return errors.Wrap(err, "add creator as member")
	}

	invites := strings.TrimSpace(c.FormValue("invites"))
	if invites != "" {
		for _, email := range strings.Split(invites, ",") {
			email = strings.TrimSpace(email)
			if email != "" {
				_ = q.AddChatMember(r.Context(), dbgen.AddChatMemberParams{
					ChatID:    chat.ID,
					UserEmail: email,
					UserID:    email,
				})
			}
		}
	}

	return c.Redirect(http.StatusFound, fmt.Sprintf("/chats/%d", chat.ID))
}

func (s *Server) HandleChatSendMessage(c echo.Context) error {
	r := c.Request()
	userID := c.Get("userID").(string)
	userEmail := c.Get("userEmail").(string)

	chatID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid chat id")
	}

	q := dbgen.New(s.DB)

	isMember, err := q.IsChatMember(r.Context(), dbgen.IsChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil || !isMember {
		return echo.NewHTTPError(http.StatusForbidden, "not a member")
	}

	content := strings.TrimSpace(c.FormValue("content"))
	if content == "" {
		return c.Redirect(http.StatusFound, fmt.Sprintf("/chats/%d", chatID))
	}

	msg, err := q.CreateMessage(r.Context(), dbgen.CreateMessageParams{
		ChatID:      chatID,
		Content:     content,
		SenderEmail: userEmail,
		SenderID:    userID,
	})
	if err != nil {
		return errors.Wrap(err, "create message")
	}

	_ = q.TouchChat(r.Context(), chatID)

	var buf bytes.Buffer
	mv := pages.MessageView{
		Content:     msg.Content,
		ID:          msg.ID,
		IsSent:      false,
		SenderEmail: msg.SenderEmail,
		SenderName:  emailToName(msg.SenderEmail),
		Time:        formatTime(msg.CreatedAt),
	}
	if err := pages.MessageBubble(mv).Render(context.Background(), &buf); err != nil {
		slog.Warn("render message bubble", "error", err)
	}

	eventData := fmt.Sprintf("sender:%s\n%s", userID, buf.String())
	s.Broker.Publish(chatID, []byte(eventData))

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		mv.IsSent = true
		return pages.MessageBubble(mv).Render(r.Context(), c.Response())
	}

	return c.Redirect(http.StatusFound, fmt.Sprintf("/chats/%d", chatID))
}

func (s *Server) HandleChatSSE(c echo.Context) error {
	userID := c.Get("userID").(string)

	chatID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid chat id")
	}

	q := dbgen.New(s.DB)
	isMember, err := q.IsChatMember(c.Request().Context(), dbgen.IsChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil || !isMember {
		return echo.NewHTTPError(http.StatusForbidden, "not a member")
	}

	c.Response().Header().Set("Content-Type", "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().WriteHeader(http.StatusOK)
	c.Response().Flush()

	ch := s.Broker.Subscribe(chatID)
	defer s.Broker.Unsubscribe(chatID, ch)

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case data := <-ch:
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				fmt.Fprintf(c.Response(), "data: %s\n", line)
			}
			fmt.Fprintf(c.Response(), "\n")
			c.Response().Flush()
		}
	}
}

func (s *Server) HandleChatAddMember(c echo.Context) error {
	r := c.Request()
	userID := c.Get("userID").(string)

	chatID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid chat id")
	}

	q := dbgen.New(s.DB)
	isMember, err := q.IsChatMember(r.Context(), dbgen.IsChatMemberParams{ChatID: chatID, UserID: userID})
	if err != nil || !isMember {
		return echo.NewHTTPError(http.StatusForbidden, "not a member")
	}

	email := strings.TrimSpace(c.FormValue("email"))
	if email == "" {
		return c.Redirect(http.StatusFound, fmt.Sprintf("/chats/%d", chatID))
	}

	_ = q.AddChatMember(r.Context(), dbgen.AddChatMemberParams{
		ChatID:    chatID,
		UserEmail: email,
		UserID:    email,
	})

	return c.Redirect(http.StatusFound, fmt.Sprintf("/chats/%d", chatID))
}

func emailToName(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) == 0 {
		return email
	}
	name := parts[0]
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	words := strings.Fields(name)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func formatTime(t interface{}) string {
	var ts time.Time
	switch v := t.(type) {
	case time.Time:
		ts = v
	case string:
		parsed, err := time.Parse("2006-01-02 15:04:05 -0700 UTC", v)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, v)
		}
		if err != nil {
			return v
		}
		ts = parsed
	default:
		return fmt.Sprintf("%v", v)
	}

	now := time.Now().UTC()
	diff := now.Sub(ts)
	switch {
	case diff < time.Minute:
		return "now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh", int(diff.Hours()))
	default:
		return ts.Format("Jan 2, 3:04 PM")
	}
}
