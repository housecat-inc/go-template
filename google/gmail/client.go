package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	defaultBaseURL       = "https://gmail.googleapis.com/gmail/v1/users/me"
	htmlBodyTruncateLimit = 20000
	maxBatchSize          = 25
)

var (
	lowValuePlaceholders = []string{
		"view this email in your browser",
		"view this message in your browser",
		"having trouble viewing this email",
		"click here to view",
		"view as web page",
		"view in browser",
		"view online",
	}
	lowValueFooterMarkers = []string{
		"unsubscribe",
		"manage preferences",
		"opt out",
		"email preferences",
	}
	metadataHeaders = []string{
		"Cc",
		"Date",
		"From",
		"In-Reply-To",
		"Message-ID",
		"References",
		"Subject",
		"To",
	}
)

type Client struct {
	BaseURL string
	Token   string
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

// Gmail API types

type Attachment struct {
	AttachmentID string `json:"attachmentId,omitempty"`
	Filename     string `json:"filename"`
	MimeType     string `json:"mimeType"`
	Size         int    `json:"size"`
}

type AttachmentData struct {
	Data string `json:"data"`
	Size int    `json:"size"`
}

type AttachmentInput struct {
	Content  string `json:"content"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type,omitempty"`
}

type Filter struct {
	Action   FilterAction   `json:"action"`
	Criteria FilterCriteria `json:"criteria"`
	ID       string         `json:"id"`
}

type FilterAction struct {
	AddLabelIDs    []string `json:"addLabelIds,omitempty"`
	Forward        string   `json:"forward,omitempty"`
	RemoveLabelIDs []string `json:"removeLabelIds,omitempty"`
}

type FilterCriteria struct {
	ExcludeChats   bool   `json:"excludeChats,omitempty"`
	From           string `json:"from,omitempty"`
	HasAttachment  bool   `json:"hasAttachment,omitempty"`
	NegatedQuery   string `json:"negatedQuery,omitempty"`
	Query          string `json:"query,omitempty"`
	Size           int    `json:"size,omitempty"`
	SizeComparison string `json:"sizeComparison,omitempty"`
	Subject        string `json:"subject,omitempty"`
	To             string `json:"to,omitempty"`
}

type Label struct {
	ID                    string `json:"id"`
	LabelListVisibility   string `json:"labelListVisibility,omitempty"`
	MessageListVisibility string `json:"messageListVisibility,omitempty"`
	Name                  string `json:"name"`
	Type                  string `json:"type,omitempty"`
}

type Message struct {
	HistoryID string       `json:"historyId,omitempty"`
	ID        string       `json:"id"`
	LabelIDs  []string     `json:"labelIds,omitempty"`
	Payload   *MessagePart `json:"payload,omitempty"`
	Snippet   string       `json:"snippet,omitempty"`
	ThreadID  string       `json:"threadId,omitempty"`
}

type MessageBody struct {
	AttachmentID string `json:"attachmentId,omitempty"`
	Data         string `json:"data,omitempty"`
	Size         int    `json:"size"`
}

type MessageHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type MessagePart struct {
	Body     *MessageBody  `json:"body,omitempty"`
	Filename string        `json:"filename,omitempty"`
	Headers  []MessageHeader `json:"headers,omitempty"`
	MimeType string        `json:"mimeType,omitempty"`
	Parts    []MessagePart `json:"parts,omitempty"`
}

type SendAs struct {
	DisplayName string `json:"displayName,omitempty"`
	IsDefault   bool   `json:"isDefault,omitempty"`
	IsPrimary   bool   `json:"isPrimary,omitempty"`
	SendAsEmail string `json:"sendAsEmail,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

type Thread struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages,omitempty"`
	Snippet  string    `json:"snippet,omitempty"`
}

// HTTP helpers

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (json.RawMessage, error) {
	apiURL := c.baseURL() + path
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
		return nil, errors.Wrap(err, "do request")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}

	slog.Info("gmail api request", "method", method, "path", path, "status", resp.StatusCode, "latency", latency.Round(time.Millisecond).String())

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, errors.Newf("gmail api %d: %s", apiErr.Error.Code, apiErr.Error.Message)
		}
		return nil, errors.Newf("gmail api %d: %s", resp.StatusCode, string(data))
	}

	return json.RawMessage(data), nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, "")
}

func (c *Client) post(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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

func (c *Client) put(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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
	return c.do(ctx, http.MethodPut, path, query, body, ct)
}

func (c *Client) del(ctx context.Context, path string) (json.RawMessage, error) {
	return c.do(ctx, http.MethodDelete, path, nil, nil, "")
}

// MIME/body helpers

func extractHeaders(part *MessagePart, names []string) map[string]string {
	if part == nil {
		return nil
	}
	target := make(map[string]string, len(names))
	for _, n := range names {
		target[strings.ToLower(n)] = n
	}
	result := make(map[string]string, len(names))
	for _, h := range part.Headers {
		if canonical, ok := target[strings.ToLower(h.Name)]; ok {
			result[canonical] = h.Value
		}
	}
	return result
}

func extractBodies(part *MessagePart) (textBody, htmlBody string) {
	if part == nil {
		return "", ""
	}
	queue := []*MessagePart{part}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p.Body != nil && p.Body.Data != "" {
			decoded := decodeBase64(p.Body.Data)
			switch p.MimeType {
			case "text/plain":
				if textBody == "" {
					textBody = decoded
				}
			case "text/html":
				if htmlBody == "" {
					htmlBody = decoded
				}
			}
		}
		if strings.HasPrefix(p.MimeType, "multipart/") {
			for i := range p.Parts {
				queue = append(queue, &p.Parts[i])
			}
		}
	}
	if part.Body != nil && part.Body.Data != "" {
		decoded := decodeBase64(part.Body.Data)
		switch part.MimeType {
		case "text/plain":
			if textBody == "" {
				textBody = decoded
			}
		case "text/html":
			if htmlBody == "" {
				htmlBody = decoded
			}
		}
	}
	return textBody, htmlBody
}

func extractAttachments(part *MessagePart) []Attachment {
	if part == nil {
		return nil
	}
	var attachments []Attachment
	var search func(p *MessagePart)
	search = func(p *MessagePart) {
		if p.Filename != "" && p.Body != nil && p.Body.AttachmentID != "" {
			attachments = append(attachments, Attachment{
				AttachmentID: p.Body.AttachmentID,
				Filename:     p.Filename,
				MimeType:     p.MimeType,
				Size:         p.Body.Size,
			})
		}
		for i := range p.Parts {
			search(&p.Parts[i])
		}
	}
	search(part)
	return attachments
}

func htmlToText(html string) string {
	if html == "" {
		return ""
	}
	var b strings.Builder
	skip := false
	inTag := false
	tagName := ""
	collectTag := false

	for _, r := range html {
		if r == '<' {
			inTag = true
			collectTag = true
			tagName = ""
			continue
		}
		if inTag {
			if r == '>' {
				inTag = false
				lower := strings.ToLower(strings.TrimPrefix(tagName, "/"))
				if strings.HasPrefix(tagName, "/") && (lower == "script" || lower == "style") {
					skip = false
				} else if lower == "script" || lower == "style" {
					skip = true
				}
				collectTag = false
				continue
			}
			if collectTag {
				if r == ' ' {
					collectTag = false
				} else {
					tagName += string(r)
				}
			}
			continue
		}
		if !skip {
			b.WriteRune(r)
		}
	}

	text := b.String()
	words := strings.Fields(text)
	return strings.Join(words, " ")
}

func formatBodyContent(textBody, htmlBody string) string {
	textStripped := strings.TrimSpace(textBody)
	htmlStripped := strings.TrimSpace(htmlBody)

	htmlText := ""
	if htmlStripped != "" {
		htmlText = strings.TrimSpace(htmlToText(htmlStripped))
	}

	plainLower := strings.ToLower(strings.Join(strings.Fields(textStripped), " "))
	htmlLower := strings.ToLower(strings.Join(strings.Fields(htmlText), " "))

	plainIsLowValue := plainLower != "" && isLowValueText(plainLower, htmlLower)

	useHTML := htmlText != "" && (textStripped == "" || strings.Contains(textStripped, "<!--") || plainIsLowValue)

	if useHTML {
		if len(htmlText) > htmlBodyTruncateLimit {
			return htmlText[:htmlBodyTruncateLimit] + "\n\n[Content truncated...]"
		}
		return htmlText
	}
	if textStripped != "" {
		return textBody
	}
	return "[No readable content found]"
}

func isLowValueText(plainLower, htmlLower string) bool {
	for _, marker := range lowValuePlaceholders {
		if strings.Contains(plainLower, marker) {
			return true
		}
	}
	const minDiff = 50
	for _, marker := range lowValueFooterMarkers {
		if strings.Contains(plainLower, marker) && len(htmlLower) >= len(plainLower)+minDiff {
			return true
		}
	}
	if len(htmlLower) >= len(plainLower)+minDiff && strings.HasSuffix(htmlLower, plainLower) {
		return true
	}
	return false
}

func formatMessageContent(msg *Message) string {
	if msg == nil || msg.Payload == nil {
		return "[No message content]"
	}
	headers := extractHeaders(msg.Payload, metadataHeaders)
	textBody, htmlBody := extractBodies(msg.Payload)
	body := formatBodyContent(textBody, htmlBody)
	attachments := extractAttachments(msg.Payload)

	var b strings.Builder
	fmt.Fprintf(&b, "Message ID: %s\n", msg.ID)
	fmt.Fprintf(&b, "Subject: %s\n", headerOr(headers, "Subject", "(no subject)"))
	fmt.Fprintf(&b, "From: %s\n", headerOr(headers, "From", "(unknown sender)"))
	fmt.Fprintf(&b, "Date: %s\n", headerOr(headers, "Date", "(unknown date)"))
	if v := headers["Message-ID"]; v != "" {
		fmt.Fprintf(&b, "Message-ID: %s\n", v)
	}
	if v := headers["In-Reply-To"]; v != "" {
		fmt.Fprintf(&b, "In-Reply-To: %s\n", v)
	}
	if v := headers["References"]; v != "" {
		fmt.Fprintf(&b, "References: %s\n", v)
	}
	if v := headers["To"]; v != "" {
		fmt.Fprintf(&b, "To: %s\n", v)
	}
	if v := headers["Cc"]; v != "" {
		fmt.Fprintf(&b, "Cc: %s\n", v)
	}
	fmt.Fprintf(&b, "Web Link: %s\n", generateWebURL(msg.ID))

	if len(attachments) > 0 {
		b.WriteString("\nAttachments:\n")
		for _, a := range attachments {
			fmt.Fprintf(&b, "  - %s (%s, %d bytes, ID: %s)\n", a.Filename, a.MimeType, a.Size, a.AttachmentID)
		}
	}

	fmt.Fprintf(&b, "\n%s\n", body)
	return b.String()
}

func formatMessageMetadata(msg *Message) string {
	if msg == nil || msg.Payload == nil {
		return "[No message content]"
	}
	headers := extractHeaders(msg.Payload, metadataHeaders)

	var b strings.Builder
	fmt.Fprintf(&b, "Message ID: %s\n", msg.ID)
	fmt.Fprintf(&b, "Subject: %s\n", headerOr(headers, "Subject", "(no subject)"))
	fmt.Fprintf(&b, "From: %s\n", headerOr(headers, "From", "(unknown sender)"))
	fmt.Fprintf(&b, "Date: %s\n", headerOr(headers, "Date", "(unknown date)"))
	if v := headers["Message-ID"]; v != "" {
		fmt.Fprintf(&b, "Message-ID: %s\n", v)
	}
	if v := headers["In-Reply-To"]; v != "" {
		fmt.Fprintf(&b, "In-Reply-To: %s\n", v)
	}
	if v := headers["References"]; v != "" {
		fmt.Fprintf(&b, "References: %s\n", v)
	}
	if v := headers["To"]; v != "" {
		fmt.Fprintf(&b, "To: %s\n", v)
	}
	if v := headers["Cc"]; v != "" {
		fmt.Fprintf(&b, "Cc: %s\n", v)
	}
	fmt.Fprintf(&b, "Web Link: %s\n", generateWebURL(msg.ID))
	return b.String()
}

func formatThreadContent(thread *Thread) string {
	if thread == nil || len(thread.Messages) == 0 {
		return fmt.Sprintf("No messages found in thread '%s'.", thread.ID)
	}
	first := thread.Messages[0]
	firstHeaders := extractHeaders(first.Payload, metadataHeaders)
	threadSubject := headerOr(firstHeaders, "Subject", "(no subject)")

	var b strings.Builder
	fmt.Fprintf(&b, "Thread ID: %s\n", thread.ID)
	fmt.Fprintf(&b, "Subject: %s\n", threadSubject)
	fmt.Fprintf(&b, "Messages: %d\n\n", len(thread.Messages))

	for i, msg := range thread.Messages {
		if msg.Payload == nil {
			continue
		}
		headers := extractHeaders(msg.Payload, metadataHeaders)
		textBody, htmlBody := extractBodies(msg.Payload)
		body := formatBodyContent(textBody, htmlBody)

		fmt.Fprintf(&b, "=== Message %d ===\n", i+1)
		fmt.Fprintf(&b, "From: %s\n", headerOr(headers, "From", "(unknown sender)"))
		fmt.Fprintf(&b, "Date: %s\n", headerOr(headers, "Date", "(unknown date)"))
		if v := headers["Message-ID"]; v != "" {
			fmt.Fprintf(&b, "Message-ID: %s\n", v)
		}
		if v := headers["In-Reply-To"]; v != "" {
			fmt.Fprintf(&b, "In-Reply-To: %s\n", v)
		}
		if v := headers["References"]; v != "" {
			fmt.Fprintf(&b, "References: %s\n", v)
		}
		subject := headerOr(headers, "Subject", "(no subject)")
		if subject != threadSubject {
			fmt.Fprintf(&b, "Subject: %s\n", subject)
		}
		fmt.Fprintf(&b, "\n%s\n\n", body)
	}
	return b.String()
}

func generateWebURL(id string) string {
	return "https://mail.google.com/mail/u/0/#all/" + id
}

func headerOr(headers map[string]string, key, fallback string) string {
	if v, ok := headers[key]; ok && v != "" {
		return v
	}
	return fallback
}

func decodeBase64(s string) string {
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		data, err = base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			return s
		}
	}
	return string(data)
}
