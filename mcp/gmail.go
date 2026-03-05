package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cockroachdb/errors"
)

const gmailAPIBase = "https://gmail.googleapis.com/gmail/v1/users/me"

type GmailClient struct {
	Token string
}

type GmailMessage struct {
	ID        string              `json:"id"`
	HistoryID string              `json:"historyId,omitempty"`
	Labels    []string            `json:"labelIds,omitempty"`
	Payload   *GmailMessagePart   `json:"payload,omitempty"`
	Snippet   string              `json:"snippet,omitempty"`
	ThreadID  string              `json:"threadId,omitempty"`
}

type GmailMessagePart struct {
	Body    *GmailMessageBody   `json:"body,omitempty"`
	Headers []GmailHeader        `json:"headers,omitempty"`
	Parts   []GmailMessagePart   `json:"parts,omitempty"`
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
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
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, errors.Newf("gmail api error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, errors.Newf("gmail api error (%d): %s", resp.StatusCode, string(data))
	}

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

func extractBody(part *GmailMessagePart) string {
	if part == nil {
		return ""
	}
	if part.Body != nil && part.Body.Data != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(part.Body.Data)
		if err == nil {
			return string(decoded)
		}
	}
	for _, p := range part.Parts {
		if text := extractBody(&p); text != "" {
			return text
		}
	}
	return ""
}

type ListMessagesOut struct {
	Messages       []MessageSummary `json:"messages"`
	NextPageToken  string           `json:"next_page_token,omitempty"`
	ResultEstimate int              `json:"result_size_estimate"`
}

const maxListMessages = 25

func (c *GmailClient) ListMessages(ctx context.Context, query string, maxResults int, pageToken string) (ListMessagesOut, error) {
	var out ListMessagesOut
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > maxListMessages {
		maxResults = maxListMessages
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

	data, err := c.get(ctx, "/messages", params)
	if err != nil {
		return out, errors.Wrap(err, "list messages")
	}

	var resp struct {
		Messages          []struct{ ID string `json:"id"` } `json:"messages"`
		NextPageToken     string                             `json:"nextPageToken"`
		ResultSizeEstimate int                               `json:"resultSizeEstimate"`
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

type CreateDraftOut struct {
	DraftID   string `json:"draft_id"`
	MessageID string `json:"message_id"`
}

func sanitizeHeader(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}

func buildRawMessage(to, subject, body string, extraHeaders map[string]string) string {
	var buf strings.Builder
	buf.WriteString("To: " + sanitizeHeader(to) + "\r\n")
	buf.WriteString("Subject: " + sanitizeHeader(subject) + "\r\n")
	for k, v := range extraHeaders {
		buf.WriteString(sanitizeHeader(k) + ": " + sanitizeHeader(v) + "\r\n")
	}
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(body)
	return buf.String()
}

func (c *GmailClient) CreateDraft(ctx context.Context, to, subject, body string) (CreateDraftOut, error) {
	var out CreateDraftOut

	raw := buildRawMessage(to, subject, body, nil)
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))

	payload := map[string]any{
		"message": map[string]any{
			"raw": encoded,
		},
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

type SendMessageOut struct {
	ID       string   `json:"id"`
	LabelIDs []string `json:"label_ids"`
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

func (c *GmailClient) SendMessage(ctx context.Context, to, subject, body, threadID string) (SendMessageOut, error) {
	var out SendMessageOut

	var extraHeaders map[string]string
	if threadID != "" {
		extraHeaders = c.getThreadReplyHeaders(ctx, threadID)
	}

	raw := buildRawMessage(to, subject, body, extraHeaders)
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))

	payload := map[string]any{
		"raw": encoded,
	}
	if threadID != "" {
		payload["threadId"] = threadID
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
