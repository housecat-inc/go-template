package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

const gmailAPIBase = "https://gmail.googleapis.com/gmail/v1/users/me"

type GmailClient struct {
	Token string
}

type GmailMessage struct {
	ID        string            `json:"id"`
	HistoryID string            `json:"historyId,omitempty"`
	Labels    []string          `json:"labelIds,omitempty"`
	Payload   *GmailMessagePart `json:"payload,omitempty"`
	Snippet   string            `json:"snippet,omitempty"`
	ThreadID  string            `json:"threadId,omitempty"`
}

type GmailMessagePart struct {
	Body     *GmailMessageBody  `json:"body,omitempty"`
	Headers  []GmailHeader      `json:"headers,omitempty"`
	MimeType string             `json:"mimeType,omitempty"`
	Parts    []GmailMessagePart `json:"parts,omitempty"`
}

type GmailMessageBody struct {
	Data string `json:"data,omitempty"`
	Size int    `json:"size"`
}

type GmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type GmailThread struct {
	ID       string         `json:"id"`
	Messages []GmailMessage `json:"messages,omitempty"`
	Snippet  string         `json:"snippet,omitempty"`
}

type GmailLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type GmailDraft struct {
	ID      string       `json:"id"`
	Message GmailMessage `json:"message"`
}

type GmailProfile struct {
	EmailAddress  string `json:"emailAddress"`
	HistoryID     string `json:"historyId"`
	MessagesTotal int    `json:"messagesTotal"`
	ThreadsTotal  int    `json:"threadsTotal"`
}

func (c *GmailClient) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (json.RawMessage, error) {
	apiURL := gmailAPIBase + path
	if len(query) > 0 {
		apiURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		slog.Error("gmail api request failed", "method", method, "path", path, "latency", latency.Round(time.Millisecond).String(), "error", err)
		return nil, errors.Wrap(err, "gmail api request")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		errMsg := string(data)
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error.Message != "" {
			errMsg = apiErr.Error.Message
		}
		slog.Warn("gmail api error", "method", method, "path", path, "status", resp.StatusCode, "error", errMsg, "latency", latency.Round(time.Millisecond).String())
		return nil, errors.Newf("gmail api error (%d): %s", resp.StatusCode, errMsg)
	}

	slog.Info("gmail api ok", "method", method, "path", path, "status", resp.StatusCode, "latency", latency.Round(time.Millisecond).String())
	return json.RawMessage(data), nil
}

func (c *GmailClient) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, "")
}

func (c *GmailClient) post(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
	var body io.Reader
	var ct string
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.Wrap(err, "marshal payload")
		}
		body = bytes.NewReader(data)
		ct = "application/json"
	}
	return c.do(ctx, http.MethodPost, path, query, body, ct)
}

type MessageSummary struct {
	Date    string `json:"date,omitempty"`
	From    string `json:"from,omitempty"`
	ID      string `json:"id"`
	Snippet string `json:"snippet"`
	Subject string `json:"subject,omitempty"`
	To      string `json:"to,omitempty"`
}

func extractHeaders(msg *GmailMessage) (from, to, subject, date string) {
	if msg.Payload == nil {
		return
	}
	for _, h := range msg.Payload.Headers {
		switch strings.ToLower(h.Name) {
		case "date":
			date = h.Value
		case "from":
			from = h.Value
		case "subject":
			subject = h.Value
		case "to":
			to = h.Value
		}
	}
	return
}

func decodeBodyData(part *GmailMessagePart) string {
	if part == nil || part.Body == nil || part.Body.Data == "" {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(part.Body.Data)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func findPartByMime(part *GmailMessagePart, mimeType string) string {
	if part == nil {
		return ""
	}
	if part.MimeType == mimeType {
		return decodeBodyData(part)
	}
	for _, p := range part.Parts {
		if text := findPartByMime(&p, mimeType); text != "" {
			return text
		}
	}
	return ""
}

func stripHTMLTags(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}

func extractBody(part *GmailMessagePart) string {
	if text := findPartByMime(part, "text/plain"); text != "" {
		return text
	}
	if html := findPartByMime(part, "text/html"); html != "" {
		return stripHTMLTags(html)
	}
	return ""
}

type GetProfileOut = GmailProfile

func (c *GmailClient) GetProfile(ctx context.Context) (GetProfileOut, error) {
	var out GetProfileOut
	data, err := c.get(ctx, "/profile", nil)
	if err != nil {
		return out, errors.Wrap(err, "get profile")
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode profile")
	}
	return out, nil
}

type GmailSearchMessagesOut struct {
	Messages       []MessageSummary `json:"messages"`
	NextPageToken  string           `json:"next_page_token,omitempty"`
	ResultEstimate int              `json:"result_size_estimate"`
}

const maxSearchMessages = 500

func (c *GmailClient) SearchMessages(ctx context.Context, query string, maxResults int, pageToken string, includeSpamTrash bool) (GmailSearchMessagesOut, error) {
	var out GmailSearchMessagesOut
	if maxResults <= 0 {
		maxResults = 20
	}
	if maxResults > maxSearchMessages {
		maxResults = maxSearchMessages
	}

	params := url.Values{
		"maxResults": {fmt.Sprintf("%d", maxResults)},
	}
	if query != "" {
		params.Set("q", query)
	}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	if includeSpamTrash {
		params.Set("includeSpamTrash", "true")
	}

	data, err := c.get(ctx, "/messages", params)
	if err != nil {
		return out, errors.Wrap(err, "search messages")
	}

	var resp struct {
		Messages           []struct{ ID string `json:"id"` } `json:"messages"`
		NextPageToken      string                             `json:"nextPageToken"`
		ResultSizeEstimate int                                `json:"resultSizeEstimate"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode list")
	}

	out.NextPageToken = resp.NextPageToken
	out.ResultEstimate = resp.ResultSizeEstimate

	for _, m := range resp.Messages {
		msg, err := c.GetMessage(ctx, m.ID)
		if err != nil {
			return out, errors.Wrapf(err, "get message %s", m.ID)
		}
		out.Messages = append(out.Messages, msg)
	}

	return out, nil
}

func (c *GmailClient) GetMessage(ctx context.Context, messageID string) (MessageSummary, error) {
	var out MessageSummary
	params := url.Values{"format": {"metadata"}, "metadataHeaders": {"Date", "From", "Subject", "To"}}
	data, err := c.get(ctx, "/messages/"+messageID, params)
	if err != nil {
		return out, errors.Wrap(err, "get message")
	}

	var msg GmailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return out, errors.Wrap(err, "decode message")
	}

	from, to, subject, date := extractHeaders(&msg)
	out.Date = date
	out.From = from
	out.ID = msg.ID
	out.Snippet = msg.Snippet
	out.Subject = subject
	out.To = to
	return out, nil
}

type ReadMessageOut struct {
	Body    string `json:"body"`
	Date    string `json:"date,omitempty"`
	From    string `json:"from,omitempty"`
	ID      string `json:"id"`
	Snippet string `json:"snippet"`
	Subject string `json:"subject,omitempty"`
	To      string `json:"to,omitempty"`
}

func (c *GmailClient) ReadMessage(ctx context.Context, messageID string) (ReadMessageOut, error) {
	var out ReadMessageOut
	params := url.Values{"format": {"full"}}
	data, err := c.get(ctx, "/messages/"+messageID, params)
	if err != nil {
		return out, errors.Wrap(err, "read message")
	}

	var msg GmailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return out, errors.Wrap(err, "decode message")
	}

	from, to, subject, date := extractHeaders(&msg)
	out.Body = extractBody(msg.Payload)
	out.Date = date
	out.From = from
	out.ID = msg.ID
	out.Snippet = msg.Snippet
	out.Subject = subject
	out.To = to
	return out, nil
}

type GetThreadOut struct {
	ID       string           `json:"id"`
	Messages []MessageSummary `json:"messages"`
}

func (c *GmailClient) GetThread(ctx context.Context, threadID string) (GetThreadOut, error) {
	var out GetThreadOut
	params := url.Values{"format": {"metadata"}, "metadataHeaders": {"Date", "From", "Subject", "To"}}
	data, err := c.get(ctx, "/threads/"+threadID, params)
	if err != nil {
		return out, errors.Wrap(err, "get thread")
	}

	var thread GmailThread
	if err := json.Unmarshal(data, &thread); err != nil {
		return out, errors.Wrap(err, "decode thread")
	}

	out.ID = thread.ID
	for _, msg := range thread.Messages {
		from, to, subject, date := extractHeaders(&msg)
		out.Messages = append(out.Messages, MessageSummary{
			Date:    date,
			From:    from,
			ID:      msg.ID,
			Snippet: msg.Snippet,
			Subject: subject,
			To:      to,
		})
	}
	return out, nil
}

type ListDraftsOut struct {
	Drafts        []DraftSummary `json:"drafts"`
	NextPageToken string         `json:"next_page_token,omitempty"`
}

type DraftSummary struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
}

func (c *GmailClient) ListDrafts(ctx context.Context, maxResults int, pageToken string) (ListDraftsOut, error) {
	var out ListDraftsOut
	if maxResults <= 0 {
		maxResults = 20
	}

	params := url.Values{
		"maxResults": {fmt.Sprintf("%d", maxResults)},
	}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}

	data, err := c.get(ctx, "/drafts", params)
	if err != nil {
		return out, errors.Wrap(err, "list drafts")
	}

	var resp struct {
		Drafts        []GmailDraft `json:"drafts"`
		NextPageToken string       `json:"nextPageToken"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode drafts")
	}

	out.NextPageToken = resp.NextPageToken
	for _, d := range resp.Drafts {
		out.Drafts = append(out.Drafts, DraftSummary{
			ID:        d.ID,
			MessageID: d.Message.ID,
		})
	}
	return out, nil
}

type ListLabelsOut struct {
	Labels []GmailLabel `json:"labels"`
}

func (c *GmailClient) ListLabels(ctx context.Context) (ListLabelsOut, error) {
	var out ListLabelsOut
	data, err := c.get(ctx, "/labels", nil)
	if err != nil {
		return out, errors.Wrap(err, "list labels")
	}

	var resp struct {
		Labels []GmailLabel `json:"labels"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode labels")
	}
	out.Labels = resp.Labels
	return out, nil
}

type CreateLabelOut = GmailLabel

func (c *GmailClient) CreateLabel(ctx context.Context, name string) (CreateLabelOut, error) {
	var out CreateLabelOut
	body := map[string]any{
		"name":                  name,
		"labelListVisibility":   "labelShow",
		"messageListVisibility": "show",
	}
	data, err := c.post(ctx, "/labels", nil, body)
	if err != nil {
		return out, errors.Wrap(err, "create label")
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode label")
	}
	return out, nil
}

type CreateDraftIn struct {
	Bcc         string `json:"bcc,omitempty"`
	Body        string `json:"body"`
	Cc          string `json:"cc,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Footer      string `json:"-"`
	Subject     string `json:"subject,omitempty"`
	ThreadID    string `json:"threadId,omitempty"`
	To          string `json:"to"`
}

type CreateDraftOut struct {
	DraftID   string `json:"draft_id"`
	MessageID string `json:"message_id"`
}

func sanitizeHeader(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}

func needsEncoding(s string) bool {
	for _, r := range s {
		if r > 126 {
			return true
		}
	}
	return false
}

func encodeSubject(subject string) string {
	if !needsEncoding(subject) {
		return sanitizeHeader(subject)
	}
	return mime.BEncoding.Encode("UTF-8", subject)
}

func detectContentType(contentType, body string) string {
	if contentType != "" {
		return contentType
	}
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "<") && strings.Contains(trimmed, "</") {
		return "text/html"
	}
	return "text/plain"
}

func plainToHTML(body string) string {
	body = strings.ReplaceAll(body, "&", "&amp;")
	body = strings.ReplaceAll(body, "<", "&lt;")
	body = strings.ReplaceAll(body, ">", "&gt;")
	body = strings.ReplaceAll(body, "\n", "<br>\n")
	return body
}

func buildRawMessage(to, subject, body, cc, bcc, contentType, footer string, extraHeaders map[string]string) string {
	contentType = detectContentType(contentType, body)
	if contentType == "text/plain" {
		body = plainToHTML(body)
		contentType = "text/html"
	}
	body += footer
	var buf strings.Builder
	if bcc != "" {
		buf.WriteString("Bcc: " + sanitizeHeader(bcc) + "\r\n")
	}
	if cc != "" {
		buf.WriteString("Cc: " + sanitizeHeader(cc) + "\r\n")
	}
	buf.WriteString("Content-Type: " + sanitizeHeader(contentType) + "; charset=UTF-8\r\n")
	for k, v := range extraHeaders {
		buf.WriteString(sanitizeHeader(k) + ": " + sanitizeHeader(v) + "\r\n")
	}
	buf.WriteString("Subject: " + encodeSubject(subject) + "\r\n")
	buf.WriteString("To: " + sanitizeHeader(to) + "\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(body)
	return buf.String()
}

func (c *GmailClient) CreateDraft(ctx context.Context, in CreateDraftIn) (CreateDraftOut, error) {
	var out CreateDraftOut

	raw := buildRawMessage(in.To, in.Subject, in.Body, in.Cc, in.Bcc, in.ContentType, in.Footer, nil)
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))

	payload := map[string]any{
		"message": map[string]any{
			"raw": encoded,
		},
	}
	if in.ThreadID != "" {
		payload["message"].(map[string]any)["threadId"] = in.ThreadID
	}

	data, err := c.post(ctx, "/drafts", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "create draft")
	}

	var draft GmailDraft
	if err := json.Unmarshal(data, &draft); err != nil {
		return out, errors.Wrap(err, "decode draft")
	}
	out.DraftID = draft.ID
	out.MessageID = draft.Message.ID
	return out, nil
}

type DeleteDraftOut struct {
	DraftID string `json:"draft_id"`
}

func (c *GmailClient) DeleteDraft(ctx context.Context, draftID string) (DeleteDraftOut, error) {
	var out DeleteDraftOut
	_, err := c.do(ctx, http.MethodDelete, "/drafts/"+url.PathEscape(draftID), nil, nil, "")
	if err != nil {
		return out, errors.Wrap(err, "delete draft")
	}
	out.DraftID = draftID
	return out, nil
}

type ModifyLabelsIn struct {
	AddLabelIDs    []string `json:"addLabelIds,omitempty"`
	MessageID      string   `json:"-"`
	RemoveLabelIDs []string `json:"removeLabelIds,omitempty"`
}

type ModifyLabelsOut struct {
	ID       string   `json:"id"`
	LabelIDs []string `json:"label_ids"`
}

func (c *GmailClient) ModifyLabels(ctx context.Context, in ModifyLabelsIn) (ModifyLabelsOut, error) {
	var out ModifyLabelsOut
	payload := map[string]any{}
	if len(in.AddLabelIDs) > 0 {
		payload["addLabelIds"] = in.AddLabelIDs
	}
	if len(in.RemoveLabelIDs) > 0 {
		payload["removeLabelIds"] = in.RemoveLabelIDs
	}
	data, err := c.post(ctx, "/messages/"+url.PathEscape(in.MessageID)+"/modify", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "modify labels")
	}
	var msg GmailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return out, errors.Wrap(err, "decode modified message")
	}
	out.ID = msg.ID
	out.LabelIDs = msg.Labels
	return out, nil
}

type SendMessageIn struct {
	Bcc         string `json:"bcc,omitempty"`
	Body        string `json:"body,omitempty"`
	Cc          string `json:"cc,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	DraftID     string `json:"draftId,omitempty"`
	Footer      string `json:"-"`
	Subject     string `json:"subject,omitempty"`
	ThreadID    string `json:"threadId,omitempty"`
	To          string `json:"to,omitempty"`
}

type SendMessageOut struct {
	ID       string   `json:"id"`
	LabelIDs []string `json:"label_ids,omitempty"`
	ThreadID string   `json:"thread_id"`
}

func (c *GmailClient) getThreadReplyHeaders(ctx context.Context, threadID string) map[string]string {
	params := url.Values{"format": {"metadata"}, "metadataHeaders": {"Message-ID"}}
	data, err := c.get(ctx, "/threads/"+threadID, params)
	if err != nil {
		return nil
	}

	var thread GmailThread
	if err := json.Unmarshal(data, &thread); err != nil || len(thread.Messages) == 0 {
		return nil
	}

	last := thread.Messages[len(thread.Messages)-1]
	if last.Payload == nil {
		return nil
	}
	for _, h := range last.Payload.Headers {
		if strings.EqualFold(h.Name, "Message-ID") && h.Value != "" {
			return map[string]string{
				"In-Reply-To": h.Value,
				"References":  h.Value,
			}
		}
	}
	return nil
}

func (c *GmailClient) SendDraft(ctx context.Context, draftID string) (SendMessageOut, error) {
	var out SendMessageOut
	payload := map[string]any{"id": draftID}
	data, err := c.post(ctx, "/drafts/send", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "send draft")
	}
	var msg GmailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return out, errors.Wrap(err, "decode sent message")
	}
	out.ID = msg.ID
	out.LabelIDs = msg.Labels
	out.ThreadID = msg.ThreadID
	return out, nil
}

type TrashMessageOut struct {
	ID string `json:"id"`
}

func (c *GmailClient) TrashMessage(ctx context.Context, messageID string) (TrashMessageOut, error) {
	var out TrashMessageOut
	data, err := c.post(ctx, "/messages/"+url.PathEscape(messageID)+"/trash", nil, nil)
	if err != nil {
		return out, errors.Wrap(err, "trash message")
	}
	var msg GmailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return out, errors.Wrap(err, "decode trashed message")
	}
	out.ID = msg.ID
	return out, nil
}

func (c *GmailClient) SendMessage(ctx context.Context, in SendMessageIn) (SendMessageOut, error) {
	var out SendMessageOut

	var extraHeaders map[string]string
	if in.ThreadID != "" {
		extraHeaders = c.getThreadReplyHeaders(ctx, in.ThreadID)
	}

	raw := buildRawMessage(in.To, in.Subject, in.Body, in.Cc, in.Bcc, in.ContentType, in.Footer, extraHeaders)
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))

	payload := map[string]any{"raw": encoded}
	if in.ThreadID != "" {
		payload["threadId"] = in.ThreadID
	}

	data, err := c.post(ctx, "/messages/send", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "send message")
	}
	var msg GmailMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return out, errors.Wrap(err, "decode sent message")
	}
	out.ID = msg.ID
	out.LabelIDs = msg.Labels
	out.ThreadID = msg.ThreadID
	return out, nil
}
