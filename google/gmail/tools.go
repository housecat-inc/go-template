package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/cockroachdb/errors"
)

// 1. SearchMessages

type SearchMessagesIn struct {
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
	Query     string `json:"query,omitempty"`
}

func (c *Client) SearchMessages(ctx context.Context, in SearchMessagesIn) (string, error) {
	q := url.Values{}
	if in.Query != "" {
		q.Set("q", in.Query)
	}
	if in.PageSize > 0 {
		q.Set("maxResults", fmt.Sprintf("%d", in.PageSize))
	} else {
		q.Set("maxResults", "10")
	}
	if in.PageToken != "" {
		q.Set("pageToken", in.PageToken)
	}

	data, err := c.get(ctx, "/messages", q)
	if err != nil {
		return "", errors.Wrap(err, "search messages")
	}

	var resp struct {
		Messages           []struct{ ID, ThreadID string } `json:"messages"`
		NextPageToken      string                          `json:"nextPageToken"`
		ResultSizeEstimate int                             `json:"resultSizeEstimate"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", errors.Wrap(err, "unmarshal search results")
	}

	if len(resp.Messages) == 0 {
		return "No messages found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d messages (estimated %d total):\n\n", len(resp.Messages), resp.ResultSizeEstimate)
	for _, m := range resp.Messages {
		fmt.Fprintf(&b, "Message ID: %s\nThread ID: %s\nWeb Link: %s\n\n", m.ID, m.ThreadID, generateWebURL(m.ID))
	}
	if resp.NextPageToken != "" {
		fmt.Fprintf(&b, "Next page token: %s\n", resp.NextPageToken)
	}
	return b.String(), nil
}

// 2. GetMessageContent

type GetMessageContentIn struct {
	MessageID string `json:"message_id"`
}

func (c *Client) GetMessageContent(ctx context.Context, in GetMessageContentIn) (string, error) {
	if in.MessageID == "" {
		return "", errors.New("message_id is required")
	}
	q := url.Values{"format": {"full"}}
	data, err := c.get(ctx, "/messages/"+in.MessageID, q)
	if err != nil {
		return "", errors.Wrap(err, "get message content")
	}
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return "", errors.Wrap(err, "unmarshal message")
	}
	return formatMessageContent(&msg), nil
}

// 3. GetMessagesContentBatch

type GetMessagesContentBatchIn struct {
	Format     string   `json:"format,omitempty"`
	MessageIDs []string `json:"message_ids"`
}

func (c *Client) GetMessagesContentBatch(ctx context.Context, in GetMessagesContentBatchIn) (string, error) {
	if len(in.MessageIDs) == 0 {
		return "", errors.New("no message IDs provided")
	}
	if len(in.MessageIDs) > maxBatchSize {
		return "", errors.Newf("batch size %d exceeds max %d", len(in.MessageIDs), maxBatchSize)
	}
	if in.Format == "" {
		in.Format = "full"
	}

	type result struct {
		idx int
		out string
		err error
	}
	results := make([]result, len(in.MessageIDs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for i, mid := range in.MessageIDs {
		wg.Add(1)
		go func(idx int, messageID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			q := url.Values{"format": {in.Format}}
			if in.Format == "metadata" {
				q.Set("metadataHeaders", strings.Join(metadataHeaders, ","))
			}
			data, err := c.get(ctx, "/messages/"+messageID, q)
			if err != nil {
				results[idx] = result{idx: idx, err: err}
				return
			}
			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				results[idx] = result{idx: idx, err: err}
				return
			}
			if in.Format == "metadata" {
				results[idx] = result{idx: idx, out: formatMessageMetadata(&msg)}
			} else {
				results[idx] = result{idx: idx, out: formatMessageContent(&msg)}
			}
		}(i, mid)
	}
	wg.Wait()

	var parts []string
	for _, r := range results {
		if r.err != nil {
			parts = append(parts, fmt.Sprintf("Message %s: %s", in.MessageIDs[r.idx], r.err))
		} else {
			parts = append(parts, r.out)
		}
	}

	return fmt.Sprintf("Retrieved %d messages:\n\n%s", len(in.MessageIDs), strings.Join(parts, "\n---\n\n")), nil
}

// 4. GetAttachmentContent

type GetAttachmentContentIn struct {
	AttachmentID string `json:"attachment_id"`
	MessageID    string `json:"message_id"`
}

func (c *Client) GetAttachmentContent(ctx context.Context, in GetAttachmentContentIn) (string, error) {
	if in.MessageID == "" {
		return "", errors.New("message_id is required")
	}
	if in.AttachmentID == "" {
		return "", errors.New("attachment_id is required")
	}

	path := fmt.Sprintf("/messages/%s/attachments/%s", in.MessageID, in.AttachmentID)
	data, err := c.get(ctx, path, nil)
	if err != nil {
		return "", errors.Wrap(err, "get attachment")
	}

	var att AttachmentData
	if err := json.Unmarshal(data, &att); err != nil {
		return "", errors.Wrap(err, "unmarshal attachment")
	}

	return fmt.Sprintf("Attachment data:\nSize: %d bytes\nContent (base64url): %s", att.Size, att.Data), nil
}

// 5. GetThreadContent

type GetThreadContentIn struct {
	ThreadID string `json:"thread_id"`
}

func (c *Client) GetThreadContent(ctx context.Context, in GetThreadContentIn) (string, error) {
	if in.ThreadID == "" {
		return "", errors.New("thread_id is required")
	}
	q := url.Values{"format": {"full"}}
	data, err := c.get(ctx, "/threads/"+in.ThreadID, q)
	if err != nil {
		return "", errors.Wrap(err, "get thread")
	}
	var thread Thread
	if err := json.Unmarshal(data, &thread); err != nil {
		return "", errors.Wrap(err, "unmarshal thread")
	}
	return formatThreadContent(&thread), nil
}

// 6. GetThreadsContentBatch

type GetThreadsContentBatchIn struct {
	ThreadIDs []string `json:"thread_ids"`
}

func (c *Client) GetThreadsContentBatch(ctx context.Context, in GetThreadsContentBatchIn) (string, error) {
	if len(in.ThreadIDs) == 0 {
		return "", errors.New("no thread IDs provided")
	}
	if len(in.ThreadIDs) > maxBatchSize {
		return "", errors.Newf("batch size %d exceeds max %d", len(in.ThreadIDs), maxBatchSize)
	}

	type result struct {
		idx int
		out string
		err error
	}
	results := make([]result, len(in.ThreadIDs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for i, tid := range in.ThreadIDs {
		wg.Add(1)
		go func(idx int, threadID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			q := url.Values{"format": {"full"}}
			data, err := c.get(ctx, "/threads/"+threadID, q)
			if err != nil {
				results[idx] = result{idx: idx, err: err}
				return
			}
			var thread Thread
			if err := json.Unmarshal(data, &thread); err != nil {
				results[idx] = result{idx: idx, err: err}
				return
			}
			results[idx] = result{idx: idx, out: formatThreadContent(&thread)}
		}(i, tid)
	}
	wg.Wait()

	var parts []string
	for _, r := range results {
		if r.err != nil {
			parts = append(parts, fmt.Sprintf("Thread %s: %s", in.ThreadIDs[r.idx], r.err))
		} else {
			parts = append(parts, r.out)
		}
	}

	return fmt.Sprintf("Retrieved %d threads:\n\n%s", len(in.ThreadIDs), strings.Join(parts, "\n---\n\n")), nil
}

// 7. ListLabels

func (c *Client) ListLabels(ctx context.Context) (string, error) {
	data, err := c.get(ctx, "/labels", nil)
	if err != nil {
		return "", errors.Wrap(err, "list labels")
	}
	var resp struct {
		Labels []Label `json:"labels"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", errors.Wrap(err, "unmarshal labels")
	}
	if len(resp.Labels) == 0 {
		return "No labels found.", nil
	}

	var system, user []Label
	for _, l := range resp.Labels {
		if l.Type == "system" {
			system = append(system, l)
		} else {
			user = append(user, l)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d labels:\n\n", len(resp.Labels))
	if len(system) > 0 {
		b.WriteString("System Labels:\n")
		for _, l := range system {
			fmt.Fprintf(&b, "  - %s (ID: %s)\n", l.Name, l.ID)
		}
		b.WriteString("\n")
	}
	if len(user) > 0 {
		b.WriteString("User Labels:\n")
		for _, l := range user {
			fmt.Fprintf(&b, "  - %s (ID: %s)\n", l.Name, l.ID)
		}
	}
	return b.String(), nil
}

// 8. ListFilters

func (c *Client) ListFilters(ctx context.Context) (string, error) {
	data, err := c.get(ctx, "/settings/filters", nil)
	if err != nil {
		return "", errors.Wrap(err, "list filters")
	}
	var resp struct {
		Filter []Filter `json:"filter"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", errors.Wrap(err, "unmarshal filters")
	}
	if len(resp.Filter) == 0 {
		return "No filters found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d filters:\n\n", len(resp.Filter))
	for _, f := range resp.Filter {
		fmt.Fprintf(&b, "Filter ID: %s\n", f.ID)
		b.WriteString("  Criteria:\n")
		criteria := formatFilterCriteria(f.Criteria)
		if len(criteria) == 0 {
			b.WriteString("    (none)\n")
		}
		for _, c := range criteria {
			fmt.Fprintf(&b, "    - %s\n", c)
		}
		b.WriteString("  Actions:\n")
		actions := formatFilterActions(f.Action)
		if len(actions) == 0 {
			b.WriteString("    (none)\n")
		}
		for _, a := range actions {
			fmt.Fprintf(&b, "    - %s\n", a)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func formatFilterCriteria(c FilterCriteria) []string {
	var lines []string
	if c.From != "" {
		lines = append(lines, "From: "+c.From)
	}
	if c.To != "" {
		lines = append(lines, "To: "+c.To)
	}
	if c.Subject != "" {
		lines = append(lines, "Subject: "+c.Subject)
	}
	if c.Query != "" {
		lines = append(lines, "Query: "+c.Query)
	}
	if c.NegatedQuery != "" {
		lines = append(lines, "Exclude Query: "+c.NegatedQuery)
	}
	if c.HasAttachment {
		lines = append(lines, "Has attachment")
	}
	if c.ExcludeChats {
		lines = append(lines, "Exclude chats")
	}
	if c.Size > 0 {
		lines = append(lines, fmt.Sprintf("Size %s %d bytes", c.SizeComparison, c.Size))
	}
	return lines
}

func formatFilterActions(a FilterAction) []string {
	var lines []string
	if a.Forward != "" {
		lines = append(lines, "Forward to: "+a.Forward)
	}
	if len(a.AddLabelIDs) > 0 {
		lines = append(lines, "Add labels: "+strings.Join(a.AddLabelIDs, ", "))
	}
	if len(a.RemoveLabelIDs) > 0 {
		lines = append(lines, "Remove labels: "+strings.Join(a.RemoveLabelIDs, ", "))
	}
	return lines
}

// 9. SendMessage

type SendMessageIn struct {
	Attachments []AttachmentInput `json:"attachments,omitempty"`
	Bcc         string            `json:"bcc,omitempty"`
	Body        string            `json:"body"`
	BodyFormat  string            `json:"body_format,omitempty"`
	Cc          string            `json:"cc,omitempty"`
	FromEmail   string            `json:"from_email,omitempty"`
	FromName    string            `json:"from_name,omitempty"`
	InReplyTo   string            `json:"in_reply_to,omitempty"`
	References  string            `json:"references,omitempty"`
	Subject     string            `json:"subject,omitempty"`
	ThreadID    string            `json:"thread_id,omitempty"`
	To          string            `json:"to"`
}

func (c *Client) SendMessage(ctx context.Context, in SendMessageIn) (string, error) {
	if in.To == "" {
		return "", errors.New("to is required")
	}
	if in.Body == "" {
		return "", errors.New("body is required")
	}

	if in.ThreadID != "" && in.InReplyTo == "" && in.References == "" {
		replyTo, refs, err := c.deriveReplyHeaders(ctx, in.ThreadID)
		if err == nil {
			in.InReplyTo = replyTo
			in.References = refs
		}
	}

	raw, attachCount, err := buildRawMessage(buildMessageParams{
		Attachments: in.Attachments,
		Bcc:         in.Bcc,
		Body:        in.Body,
		BodyFormat:  in.BodyFormat,
		Cc:          in.Cc,
		FromEmail:   in.FromEmail,
		FromName:    in.FromName,
		InReplyTo:   in.InReplyTo,
		References:  in.References,
		Subject:     in.Subject,
		To:          in.To,
	})
	if err != nil {
		return "", errors.Wrap(err, "build raw message")
	}

	payload := map[string]string{"raw": raw}
	if in.ThreadID != "" {
		payload["threadId"] = in.ThreadID
	}

	data, err := c.post(ctx, "/messages/send", nil, payload)
	if err != nil {
		return "", errors.Wrap(err, "send message")
	}

	var resp struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", errors.Wrap(err, "unmarshal send response")
	}

	result := fmt.Sprintf("Email sent successfully!\nMessage ID: %s\nThread ID: %s", resp.ID, resp.ThreadID)
	if attachCount > 0 {
		result += fmt.Sprintf("\nAttachments: %d file(s)", attachCount)
	}
	return result, nil
}

// 10. DraftMessage

type DraftMessageIn struct {
	Attachments      []AttachmentInput `json:"attachments,omitempty"`
	Bcc              string            `json:"bcc,omitempty"`
	Body             string            `json:"body"`
	BodyFormat       string            `json:"body_format,omitempty"`
	Cc               string            `json:"cc,omitempty"`
	FromEmail        string            `json:"from_email,omitempty"`
	FromName         string            `json:"from_name,omitempty"`
	IncludeSignature bool              `json:"include_signature,omitempty"`
	InReplyTo        string            `json:"in_reply_to,omitempty"`
	QuoteOriginal    bool              `json:"quote_original,omitempty"`
	References       string            `json:"references,omitempty"`
	Subject          string            `json:"subject,omitempty"`
	ThreadID         string            `json:"thread_id,omitempty"`
	To               string            `json:"to,omitempty"`
}

func (c *Client) DraftMessage(ctx context.Context, in DraftMessageIn) (string, error) {
	if in.Body == "" {
		return "", errors.New("body is required")
	}

	body := in.Body
	bodyFormat := in.BodyFormat
	if bodyFormat == "" {
		bodyFormat = "plain"
	}

	if in.ThreadID != "" && in.InReplyTo == "" && in.References == "" {
		replyTo, refs, err := c.deriveReplyHeaders(ctx, in.ThreadID)
		if err == nil {
			in.InReplyTo = replyTo
			in.References = refs
		}
	}

	if in.IncludeSignature {
		sig := c.getSendAsSignatureHTML(ctx, in.FromEmail)
		if sig != "" {
			body = appendSignatureToBody(body, bodyFormat, sig)
			bodyFormat = "html"
		}
	}

	if in.QuoteOriginal && in.ThreadID != "" {
		orig := c.fetchOriginalForQuote(ctx, in.ThreadID, in.InReplyTo)
		if orig != nil {
			body = buildQuotedReplyBody(body, bodyFormat, orig)
			bodyFormat = "html"
		}
	}

	raw, attachCount, err := buildRawMessage(buildMessageParams{
		Attachments: in.Attachments,
		Bcc:         in.Bcc,
		Body:        body,
		BodyFormat:  bodyFormat,
		Cc:          in.Cc,
		FromEmail:   in.FromEmail,
		FromName:    in.FromName,
		InReplyTo:   in.InReplyTo,
		References:  in.References,
		Subject:     in.Subject,
		To:          in.To,
	})
	if err != nil {
		return "", errors.Wrap(err, "build raw message")
	}

	message := map[string]string{"raw": raw}
	if in.ThreadID != "" {
		message["threadId"] = in.ThreadID
	}
	draftPayload := map[string]any{"message": message}

	data, err := c.post(ctx, "/drafts", nil, draftPayload)
	if err != nil {
		return "", errors.Wrap(err, "create draft")
	}

	var resp struct {
		ID      string `json:"id"`
		Message struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", errors.Wrap(err, "unmarshal draft response")
	}

	result := fmt.Sprintf("Draft created successfully!\nDraft ID: %s\nMessage ID: %s", resp.ID, resp.Message.ID)
	if attachCount > 0 {
		result += fmt.Sprintf("\nAttachments: %d file(s)", attachCount)
	}
	return result, nil
}

// 11. ManageLabel

type ManageLabelIn struct {
	Action                string `json:"action"`
	LabelID               string `json:"label_id,omitempty"`
	LabelListVisibility   string `json:"label_list_visibility,omitempty"`
	MessageListVisibility string `json:"message_list_visibility,omitempty"`
	Name                  string `json:"name,omitempty"`
}

func (c *Client) ManageLabel(ctx context.Context, in ManageLabelIn) (string, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))

	switch action {
	case "create":
		if in.Name == "" {
			return "", errors.New("label name is required for create action")
		}
		vis := in.LabelListVisibility
		if vis == "" {
			vis = "labelShow"
		}
		msgVis := in.MessageListVisibility
		if msgVis == "" {
			msgVis = "show"
		}
		payload := map[string]string{
			"labelListVisibility":   vis,
			"messageListVisibility": msgVis,
			"name":                  in.Name,
		}
		data, err := c.post(ctx, "/labels", nil, payload)
		if err != nil {
			return "", errors.Wrap(err, "create label")
		}
		var label Label
		if err := json.Unmarshal(data, &label); err != nil {
			return "", errors.Wrap(err, "unmarshal label")
		}
		return fmt.Sprintf("Label created successfully!\nName: %s\nID: %s", label.Name, label.ID), nil

	case "update":
		if in.LabelID == "" {
			return "", errors.New("label_id is required for update action")
		}
		current, err := c.get(ctx, "/labels/"+in.LabelID, nil)
		if err != nil {
			return "", errors.Wrap(err, "get current label")
		}
		var currentLabel Label
		if err := json.Unmarshal(current, &currentLabel); err != nil {
			return "", errors.Wrap(err, "unmarshal current label")
		}
		name := in.Name
		if name == "" {
			name = currentLabel.Name
		}
		vis := in.LabelListVisibility
		if vis == "" {
			vis = "labelShow"
		}
		msgVis := in.MessageListVisibility
		if msgVis == "" {
			msgVis = "show"
		}
		payload := map[string]string{
			"id":                    in.LabelID,
			"labelListVisibility":   vis,
			"messageListVisibility": msgVis,
			"name":                  name,
		}
		data, err := c.put(ctx, "/labels/"+in.LabelID, nil, payload)
		if err != nil {
			return "", errors.Wrap(err, "update label")
		}
		var label Label
		if err := json.Unmarshal(data, &label); err != nil {
			return "", errors.Wrap(err, "unmarshal label")
		}
		return fmt.Sprintf("Label updated successfully!\nName: %s\nID: %s", label.Name, label.ID), nil

	case "delete":
		if in.LabelID == "" {
			return "", errors.New("label_id is required for delete action")
		}
		current, err := c.get(ctx, "/labels/"+in.LabelID, nil)
		if err != nil {
			return "", errors.Wrap(err, "get label for delete")
		}
		var label Label
		if err := json.Unmarshal(current, &label); err != nil {
			return "", errors.Wrap(err, "unmarshal label")
		}
		_, err = c.del(ctx, "/labels/"+in.LabelID)
		if err != nil {
			return "", errors.Wrap(err, "delete label")
		}
		return fmt.Sprintf("Label '%s' (ID: %s) deleted successfully!", label.Name, in.LabelID), nil

	default:
		return "", errors.Newf("invalid action '%s'. Must be 'create', 'update', or 'delete'", action)
	}
}

// 12. ManageFilter

type ManageFilterIn struct {
	Action       string          `json:"action"`
	Criteria     *FilterCriteria `json:"criteria,omitempty"`
	FilterAction *FilterAction   `json:"filter_action,omitempty"`
	FilterID     string          `json:"filter_id,omitempty"`
}

func (c *Client) ManageFilter(ctx context.Context, in ManageFilterIn) (string, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))

	switch action {
	case "create":
		if in.Criteria == nil || in.FilterAction == nil {
			return "", errors.New("criteria and filter_action are required for create action")
		}
		payload := map[string]any{
			"action":   in.FilterAction,
			"criteria": in.Criteria,
		}
		data, err := c.post(ctx, "/settings/filters", nil, payload)
		if err != nil {
			return "", errors.Wrap(err, "create filter")
		}
		var filter Filter
		if err := json.Unmarshal(data, &filter); err != nil {
			return "", errors.Wrap(err, "unmarshal filter")
		}
		return fmt.Sprintf("Filter created successfully!\nFilter ID: %s", filter.ID), nil

	case "delete":
		if in.FilterID == "" {
			return "", errors.New("filter_id is required for delete action")
		}
		current, err := c.get(ctx, "/settings/filters/"+in.FilterID, nil)
		if err != nil {
			return "", errors.Wrap(err, "get filter for delete")
		}
		var filter Filter
		if err := json.Unmarshal(current, &filter); err != nil {
			return "", errors.Wrap(err, "unmarshal filter")
		}
		_, err = c.del(ctx, "/settings/filters/"+in.FilterID)
		if err != nil {
			return "", errors.Wrap(err, "delete filter")
		}
		return fmt.Sprintf("Filter deleted successfully!\nFilter ID: %s\nCriteria: %v\nAction: %v", in.FilterID, filter.Criteria, filter.Action), nil

	default:
		return "", errors.Newf("invalid action '%s'. Must be 'create' or 'delete'", action)
	}
}

// 13. ModifyMessageLabels

type ModifyMessageLabelsIn struct {
	AddLabelIDs    []string `json:"add_label_ids,omitempty"`
	MessageID      string   `json:"message_id"`
	RemoveLabelIDs []string `json:"remove_label_ids,omitempty"`
}

func (c *Client) ModifyMessageLabels(ctx context.Context, in ModifyMessageLabelsIn) (string, error) {
	if in.MessageID == "" {
		return "", errors.New("message_id is required")
	}
	if len(in.AddLabelIDs) == 0 && len(in.RemoveLabelIDs) == 0 {
		return "", errors.New("at least one of add_label_ids or remove_label_ids must be provided")
	}

	payload := map[string]any{}
	if len(in.AddLabelIDs) > 0 {
		payload["addLabelIds"] = in.AddLabelIDs
	}
	if len(in.RemoveLabelIDs) > 0 {
		payload["removeLabelIds"] = in.RemoveLabelIDs
	}

	data, err := c.post(ctx, "/messages/"+in.MessageID+"/modify", nil, payload)
	if err != nil {
		return "", errors.Wrap(err, "modify labels")
	}

	var resp struct {
		ID       string   `json:"id"`
		LabelIDs []string `json:"labelIds"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", errors.Wrap(err, "unmarshal modify response")
	}

	var actions []string
	if len(in.AddLabelIDs) > 0 {
		actions = append(actions, "Added labels: "+strings.Join(in.AddLabelIDs, ", "))
	}
	if len(in.RemoveLabelIDs) > 0 {
		actions = append(actions, "Removed labels: "+strings.Join(in.RemoveLabelIDs, ", "))
	}
	return fmt.Sprintf("Labels updated for message %s: %s", resp.ID, strings.Join(actions, "; ")), nil
}

// 14. BatchModifyMessageLabels

type BatchModifyMessageLabelsIn struct {
	AddLabelIDs    []string `json:"add_label_ids,omitempty"`
	MessageIDs     []string `json:"message_ids"`
	RemoveLabelIDs []string `json:"remove_label_ids,omitempty"`
}

func (c *Client) BatchModifyMessageLabels(ctx context.Context, in BatchModifyMessageLabelsIn) (string, error) {
	if len(in.MessageIDs) == 0 {
		return "", errors.New("message_ids is required")
	}
	if len(in.AddLabelIDs) == 0 && len(in.RemoveLabelIDs) == 0 {
		return "", errors.New("at least one of add_label_ids or remove_label_ids must be provided")
	}

	payload := map[string]any{
		"ids": in.MessageIDs,
	}
	if len(in.AddLabelIDs) > 0 {
		payload["addLabelIds"] = in.AddLabelIDs
	}
	if len(in.RemoveLabelIDs) > 0 {
		payload["removeLabelIds"] = in.RemoveLabelIDs
	}

	_, err := c.post(ctx, "/messages/batchModify", nil, payload)
	if err != nil {
		return "", errors.Wrap(err, "batch modify labels")
	}

	var actions []string
	if len(in.AddLabelIDs) > 0 {
		actions = append(actions, "Added labels: "+strings.Join(in.AddLabelIDs, ", "))
	}
	if len(in.RemoveLabelIDs) > 0 {
		actions = append(actions, "Removed labels: "+strings.Join(in.RemoveLabelIDs, ", "))
	}
	return fmt.Sprintf("Labels updated for %d messages: %s", len(in.MessageIDs), strings.Join(actions, "; ")), nil
}

// Message building helpers

type buildMessageParams struct {
	Attachments []AttachmentInput
	Bcc         string
	Body        string
	BodyFormat  string
	Cc          string
	FromEmail   string
	FromName    string
	InReplyTo   string
	References  string
	Subject     string
	To          string
}

func buildRawMessage(p buildMessageParams) (string, int, error) {
	if p.BodyFormat == "" {
		p.BodyFormat = "plain"
	}
	contentType := "text/plain"
	if p.BodyFormat == "html" {
		contentType = "text/html"
	}

	var buf strings.Builder

	if len(p.Attachments) == 0 {
		writeHeaders(&buf, p)
		fmt.Fprintf(&buf, "Content-Type: %s; charset=UTF-8\r\n", contentType)
		buf.WriteString("MIME-Version: 1.0\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(p.Body)
	} else {
		w := multipart.NewWriter(&buf)
		writeHeaders(&buf, p)
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%s\r\n", w.Boundary())
		buf.WriteString("MIME-Version: 1.0\r\n")
		buf.WriteString("\r\n")

		bodyHeader := make(textproto.MIMEHeader)
		bodyHeader.Set("Content-Type", contentType+"; charset=UTF-8")
		bodyPart, err := w.CreatePart(bodyHeader)
		if err != nil {
			return "", 0, errors.Wrap(err, "create body part")
		}
		bodyPart.Write([]byte(p.Body))

		for _, att := range p.Attachments {
			mimeType := att.MimeType
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			attHeader := make(textproto.MIMEHeader)
			attHeader.Set("Content-Type", mimeType)
			attHeader.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", att.Filename))
			attHeader.Set("Content-Transfer-Encoding", "base64")
			part, err := w.CreatePart(attHeader)
			if err != nil {
				return "", 0, errors.Wrap(err, "create attachment part")
			}
			part.Write([]byte(att.Content))
		}
		w.Close()
	}

	encoded := base64.URLEncoding.EncodeToString([]byte(buf.String()))
	return encoded, len(p.Attachments), nil
}

func writeHeaders(buf *strings.Builder, p buildMessageParams) {
	if p.To != "" {
		fmt.Fprintf(buf, "To: %s\r\n", sanitizeHeader(p.To))
	}
	if p.Cc != "" {
		fmt.Fprintf(buf, "Cc: %s\r\n", sanitizeHeader(p.Cc))
	}
	if p.Bcc != "" {
		fmt.Fprintf(buf, "Bcc: %s\r\n", sanitizeHeader(p.Bcc))
	}
	if p.FromEmail != "" || p.FromName != "" {
		from := p.FromEmail
		if p.FromName != "" {
			from = fmt.Sprintf("%s <%s>", p.FromName, p.FromEmail)
		}
		fmt.Fprintf(buf, "From: %s\r\n", sanitizeHeader(from))
	}
	if p.Subject != "" {
		fmt.Fprintf(buf, "Subject: %s\r\n", encodeSubject(p.Subject))
	}
	if p.InReplyTo != "" {
		fmt.Fprintf(buf, "In-Reply-To: %s\r\n", sanitizeHeader(p.InReplyTo))
	}
	if p.References != "" {
		fmt.Fprintf(buf, "References: %s\r\n", sanitizeHeader(p.References))
	}
}

func sanitizeHeader(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

func encodeSubject(s string) string {
	for _, r := range s {
		if r > 126 || !unicode.IsPrint(r) {
			return mime.BEncoding.Encode("UTF-8", s)
		}
	}
	return sanitizeHeader(s)
}

// Threading helpers

func (c *Client) deriveReplyHeaders(ctx context.Context, threadID string) (inReplyTo, references string, err error) {
	q := url.Values{
		"format":          {"metadata"},
		"metadataHeaders": {strings.Join([]string{"Message-ID", "In-Reply-To", "References"}, ",")},
	}
	data, err := c.get(ctx, "/threads/"+threadID, q)
	if err != nil {
		return "", "", errors.Wrap(err, "fetch thread for reply headers")
	}

	var thread Thread
	if err := json.Unmarshal(data, &thread); err != nil {
		return "", "", errors.Wrap(err, "unmarshal thread")
	}

	if len(thread.Messages) == 0 {
		return "", "", nil
	}

	var msgIDs []string
	for _, msg := range thread.Messages {
		if msg.Payload == nil {
			continue
		}
		headers := extractHeaders(msg.Payload, []string{"Message-ID"})
		if mid := headers["Message-ID"]; mid != "" {
			msgIDs = append(msgIDs, mid)
		}
	}

	if len(msgIDs) > 0 {
		inReplyTo = msgIDs[len(msgIDs)-1]
		references = strings.Join(msgIDs, " ")
	}

	return inReplyTo, references, nil
}

// Signature helpers

func (c *Client) getSendAsSignatureHTML(ctx context.Context, fromEmail string) string {
	data, err := c.get(ctx, "/settings/sendAs", nil)
	if err != nil {
		return ""
	}
	var resp struct {
		SendAs []SendAs `json:"sendAs"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return ""
	}
	for _, sa := range resp.SendAs {
		if fromEmail != "" && sa.SendAsEmail == fromEmail {
			return sa.Signature
		}
		if sa.IsDefault || sa.IsPrimary {
			if sa.Signature != "" {
				return sa.Signature
			}
		}
	}
	return ""
}

type originalMessage struct {
	Body string
	Date string
	From string
}

func (c *Client) fetchOriginalForQuote(ctx context.Context, threadID, inReplyTo string) *originalMessage {
	q := url.Values{"format": {"full"}}
	data, err := c.get(ctx, "/threads/"+threadID, q)
	if err != nil {
		return nil
	}
	var thread Thread
	if err := json.Unmarshal(data, &thread); err != nil || len(thread.Messages) == 0 {
		return nil
	}

	var target *Message
	if inReplyTo != "" {
		for i := range thread.Messages {
			if thread.Messages[i].Payload == nil {
				continue
			}
			h := extractHeaders(thread.Messages[i].Payload, []string{"Message-ID"})
			if h["Message-ID"] == inReplyTo {
				target = &thread.Messages[i]
				break
			}
		}
	}
	if target == nil {
		target = &thread.Messages[len(thread.Messages)-1]
	}
	if target.Payload == nil {
		return nil
	}

	headers := extractHeaders(target.Payload, []string{"Date", "From"})
	textBody, htmlBody := extractBodies(target.Payload)
	body := textBody
	if body == "" {
		body = htmlToText(htmlBody)
	}

	return &originalMessage{
		Body: body,
		Date: headers["Date"],
		From: headers["From"],
	}
}

func appendSignatureToBody(body, bodyFormat, signatureHTML string) string {
	if bodyFormat == "html" {
		return body + "<br><br>" + signatureHTML
	}
	return "<div>" + strings.ReplaceAll(body, "\n", "<br>") + "</div><br><br>" + signatureHTML
}

func buildQuotedReplyBody(replyBody, bodyFormat string, orig *originalMessage) string {
	var htmlReply string
	if bodyFormat == "html" {
		htmlReply = replyBody
	} else {
		htmlReply = "<div>" + strings.ReplaceAll(replyBody, "\n", "<br>") + "</div>"
	}

	quote := fmt.Sprintf(
		"<br><div class=\"gmail_quote\"><div>On %s, %s wrote:</div><blockquote style=\"margin:0 0 0 .8ex;border-left:1px #ccc solid;padding-left:1ex\">%s</blockquote></div>",
		orig.Date, orig.From, strings.ReplaceAll(orig.Body, "\n", "<br>"),
	)

	return htmlReply + quote
}
