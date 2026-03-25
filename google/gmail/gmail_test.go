package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock Gmail server

type mockGmailServer struct {
	mu       sync.Mutex
	drafts   map[string]json.RawMessage
	filters  map[string]json.RawMessage
	labels   map[string]json.RawMessage
	messages map[string]json.RawMessage
	threads  map[string]json.RawMessage
	sendAs   []SendAs
	srv      *httptest.Server

	lastBatchModifyBody map[string]any
	lastModifyBody      map[string]any
}

func newMockGmailServer() *mockGmailServer {
	m := &mockGmailServer{
		drafts:   make(map[string]json.RawMessage),
		filters:  make(map[string]json.RawMessage),
		labels:   make(map[string]json.RawMessage),
		messages: make(map[string]json.RawMessage),
		threads:  make(map[string]json.RawMessage),
		sendAs: []SendAs{{
			DisplayName: "Test User",
			IsDefault:   true,
			IsPrimary:   true,
			SendAsEmail: "test@example.com",
			Signature:   "<div>-- Test Signature</div>",
		}},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/messages/batchModify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		m.lastBatchModifyBody = body
		m.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/messages/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		resp := map[string]string{"id": "sent-msg-1", "threadId": "thread-1"}
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/messages/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/messages/")

		if strings.Contains(path, "/attachments/") {
			parts := strings.SplitN(path, "/attachments/", 2)
			_ = parts[0]
			json.NewEncoder(w).Encode(AttachmentData{
				Data: base64.URLEncoding.EncodeToString([]byte("file content")),
				Size: 12,
			})
			return
		}

		if strings.HasSuffix(path, "/modify") {
			msgID := strings.TrimSuffix(path, "/modify")
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			m.mu.Lock()
			m.lastModifyBody = body
			m.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{
				"id":       msgID,
				"labelIds": []string{"INBOX", "Label_1"},
			})
			return
		}

		m.mu.Lock()
		msg, ok := m.messages[path]
		m.mu.Unlock()
		if !ok {
			http.Error(w, `{"error":{"code":404,"message":"not found"}}`, http.StatusNotFound)
			return
		}
		w.Write(msg)
	})

	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		m.mu.Lock()
		var msgs []map[string]string
		for id := range m.messages {
			msgs = append(msgs, map[string]string{"id": id, "threadId": "thread-1"})
		}
		m.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{
			"messages":           msgs,
			"resultSizeEstimate": len(msgs),
		})
	})

	mux.HandleFunc("/threads/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/threads/")
		m.mu.Lock()
		thread, ok := m.threads[id]
		m.mu.Unlock()
		if !ok {
			http.Error(w, `{"error":{"code":404,"message":"not found"}}`, http.StatusNotFound)
			return
		}
		w.Write(thread)
	})

	mux.HandleFunc("/labels/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/labels/")
		m.mu.Lock()
		defer m.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			label, ok := m.labels[id]
			if !ok {
				http.Error(w, `{"error":{"code":404,"message":"not found"}}`, http.StatusNotFound)
				return
			}
			w.Write(label)
		case http.MethodPut:
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			data, _ := json.Marshal(Label{
				ID:   id,
				Name: body["name"],
			})
			m.labels[id] = data
			w.Write(data)
		case http.MethodDelete:
			delete(m.labels, id)
			w.WriteHeader(http.StatusNoContent)
		}
	})

	mux.HandleFunc("/labels", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			var labels []json.RawMessage
			for _, l := range m.labels {
				labels = append(labels, l)
			}
			json.NewEncoder(w).Encode(map[string]any{"labels": labels})
		case http.MethodPost:
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			id := "Label_" + strings.ReplaceAll(body["name"], " ", "_")
			data, _ := json.Marshal(Label{
				ID:   id,
				Name: body["name"],
			})
			m.labels[id] = data
			w.Write(data)
		}
	})

	mux.HandleFunc("/settings/filters/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/settings/filters/")
		m.mu.Lock()
		defer m.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			f, ok := m.filters[id]
			if !ok {
				http.Error(w, `{"error":{"code":404,"message":"not found"}}`, http.StatusNotFound)
				return
			}
			w.Write(f)
		case http.MethodDelete:
			delete(m.filters, id)
			w.WriteHeader(http.StatusNoContent)
		}
	})

	mux.HandleFunc("/settings/filters", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			var filters []json.RawMessage
			for _, f := range m.filters {
				filters = append(filters, f)
			}
			json.NewEncoder(w).Encode(map[string]any{"filter": filters})
		case http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			id := fmt.Sprintf("filter-%d", len(m.filters)+1)
			filter := Filter{ID: id}
			data, _ := json.Marshal(filter)
			m.filters[id] = data
			json.NewEncoder(w).Encode(filter)
		}
	})

	mux.HandleFunc("/settings/sendAs", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"sendAs": m.sendAs})
	})

	mux.HandleFunc("/drafts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "draft-1",
			"message": map[string]string{"id": "msg-draft-1"},
		})
	})

	m.srv = httptest.NewServer(mux)
	return m
}

func (m *mockGmailServer) client() *Client {
	return &Client{BaseURL: m.srv.URL, Token: "test-token"}
}

func (m *mockGmailServer) close() {
	m.srv.Close()
}

// Test fixtures

func fixtureMessage(id string) json.RawMessage {
	msg := Message{
		ID:       id,
		ThreadID: "thread-1",
		Payload: &MessagePart{
			MimeType: "multipart/mixed",
			Headers: []MessageHeader{
				{Name: "Subject", Value: "Test Subject"},
				{Name: "From", Value: "sender@example.com"},
				{Name: "To", Value: "recipient@example.com"},
				{Name: "Date", Value: "Mon, 1 Jan 2024 00:00:00 +0000"},
				{Name: "Message-ID", Value: "<msg-" + id + "@example.com>"},
			},
			Parts: []MessagePart{
				{
					MimeType: "text/plain",
					Body: &MessageBody{
						Data: base64.URLEncoding.EncodeToString([]byte("Hello, this is a test email.")),
					},
				},
				{
					MimeType: "text/html",
					Body: &MessageBody{
						Data: base64.URLEncoding.EncodeToString([]byte("<p>Hello, this is a <b>test</b> email.</p>")),
					},
				},
				{
					MimeType: "application/pdf",
					Filename: "report.pdf",
					Body: &MessageBody{
						AttachmentID: "att-1",
						Size:         1024,
					},
				},
			},
		},
	}
	data, _ := json.Marshal(msg)
	return data
}

func fixtureThread(id string, messageCount int) json.RawMessage {
	thread := Thread{ID: id}
	for i := 0; i < messageCount; i++ {
		mid := fmt.Sprintf("%s-msg-%d", id, i+1)
		thread.Messages = append(thread.Messages, Message{
			ID:       mid,
			ThreadID: id,
			Payload: &MessagePart{
				MimeType: "text/plain",
				Headers: []MessageHeader{
					{Name: "Subject", Value: "Thread Subject"},
					{Name: "From", Value: fmt.Sprintf("user%d@example.com", i+1)},
					{Name: "Date", Value: fmt.Sprintf("Mon, %d Jan 2024 00:00:00 +0000", i+1)},
					{Name: "Message-ID", Value: fmt.Sprintf("<msg-%s@example.com>", mid)},
				},
				Body: &MessageBody{
					Data: base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("Message %d body", i+1))),
				},
			},
		})
	}
	data, _ := json.Marshal(thread)
	return data
}

// Tests

func TestSearchMessages(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()
	ctx := context.Background()

	m.mu.Lock()
	m.messages["msg-1"] = fixtureMessage("msg-1")
	m.messages["msg-2"] = fixtureMessage("msg-2")
	m.mu.Unlock()

	result, err := c.SearchMessages(ctx, SearchMessagesIn{Query: "test", PageSize: 10})
	a.NoError(err)
	a.Contains(result, "Found 2 messages")
	a.Contains(result, "msg-1")
	a.Contains(result, "msg-2")
	a.Contains(result, "mail.google.com")
}

func TestSearchMessagesEmpty(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()

	result, err := m.client().SearchMessages(context.Background(), SearchMessagesIn{Query: "nonexistent"})
	a.NoError(err)
	a.Equal("No messages found.", result)
}

func TestGetMessageContent(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	m.messages["msg-1"] = fixtureMessage("msg-1")
	m.mu.Unlock()

	result, err := c.GetMessageContent(context.Background(), GetMessageContentIn{MessageID: "msg-1"})
	a.NoError(err)
	a.Contains(result, "Message ID: msg-1")
	a.Contains(result, "Subject: Test Subject")
	a.Contains(result, "From: sender@example.com")
	a.Contains(result, "To: recipient@example.com")
	a.Contains(result, "Hello, this is a test email.")
	a.Contains(result, "report.pdf")
	a.Contains(result, "att-1")
}

func TestGetMessageContentNotFound(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()

	_, err := m.client().GetMessageContent(context.Background(), GetMessageContentIn{MessageID: "nonexistent"})
	a.Error(err)
	a.Contains(err.Error(), "404")
}

func TestGetMessageContentValidation(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()

	_, err := m.client().GetMessageContent(context.Background(), GetMessageContentIn{})
	a.Error(err)
	a.Contains(err.Error(), "message_id is required")
}

func TestGetMessagesContentBatch(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	m.messages["msg-1"] = fixtureMessage("msg-1")
	m.messages["msg-2"] = fixtureMessage("msg-2")
	m.mu.Unlock()

	result, err := c.GetMessagesContentBatch(context.Background(), GetMessagesContentBatchIn{
		MessageIDs: []string{"msg-1", "msg-2"},
	})
	a.NoError(err)
	a.Contains(result, "Retrieved 2 messages")
	a.Contains(result, "msg-1")
	a.Contains(result, "msg-2")
	a.Contains(result, "---")
}

func TestGetMessagesContentBatchMaxExceeded(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()

	ids := make([]string, 26)
	for i := range ids {
		ids[i] = fmt.Sprintf("msg-%d", i)
	}
	_, err := m.client().GetMessagesContentBatch(context.Background(), GetMessagesContentBatchIn{MessageIDs: ids})
	a.Error(err)
	a.Contains(err.Error(), "exceeds max 25")
}

func TestGetMessagesContentBatchMetadata(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	m.messages["msg-1"] = fixtureMessage("msg-1")
	m.mu.Unlock()

	result, err := c.GetMessagesContentBatch(context.Background(), GetMessagesContentBatchIn{
		MessageIDs: []string{"msg-1"},
		Format:     "metadata",
	})
	a.NoError(err)
	a.Contains(result, "Subject: Test Subject")
	a.Contains(result, "From: sender@example.com")
}

func TestGetAttachmentContent(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()

	result, err := m.client().GetAttachmentContent(context.Background(), GetAttachmentContentIn{
		MessageID:    "msg-1",
		AttachmentID: "att-1",
	})
	a.NoError(err)
	a.Contains(result, "Size: 12 bytes")
}

func TestGetAttachmentContentValidation(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	_, err := c.GetAttachmentContent(context.Background(), GetAttachmentContentIn{})
	a.Error(err)
	a.Contains(err.Error(), "message_id is required")

	_, err = c.GetAttachmentContent(context.Background(), GetAttachmentContentIn{MessageID: "msg-1"})
	a.Error(err)
	a.Contains(err.Error(), "attachment_id is required")
}

func TestGetThreadContent(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	m.threads["thread-1"] = fixtureThread("thread-1", 3)
	m.mu.Unlock()

	result, err := c.GetThreadContent(context.Background(), GetThreadContentIn{ThreadID: "thread-1"})
	a.NoError(err)
	a.Contains(result, "Thread ID: thread-1")
	a.Contains(result, "Messages: 3")
	a.Contains(result, "=== Message 1 ===")
	a.Contains(result, "=== Message 3 ===")
	a.Contains(result, "user1@example.com")
	a.Contains(result, "Message 1 body")
}

func TestGetThreadsContentBatch(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	m.threads["t1"] = fixtureThread("t1", 2)
	m.threads["t2"] = fixtureThread("t2", 1)
	m.mu.Unlock()

	result, err := c.GetThreadsContentBatch(context.Background(), GetThreadsContentBatchIn{
		ThreadIDs: []string{"t1", "t2"},
	})
	a.NoError(err)
	a.Contains(result, "Retrieved 2 threads")
	a.Contains(result, "t1")
	a.Contains(result, "t2")
}

func TestListLabels(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	inboxData, _ := json.Marshal(Label{ID: "INBOX", Name: "INBOX", Type: "system"})
	customData, _ := json.Marshal(Label{ID: "Label_1", Name: "Work", Type: "user"})
	m.labels["INBOX"] = inboxData
	m.labels["Label_1"] = customData
	m.mu.Unlock()

	result, err := c.ListLabels(context.Background())
	a.NoError(err)
	a.Contains(result, "Found 2 labels")
	a.Contains(result, "System Labels:")
	a.Contains(result, "INBOX")
	a.Contains(result, "User Labels:")
	a.Contains(result, "Work")
}

func TestListFilters(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	f := Filter{
		ID:       "filter-1",
		Criteria: FilterCriteria{From: "boss@example.com", HasAttachment: true},
		Action:   FilterAction{AddLabelIDs: []string{"Label_Important"}},
	}
	fData, _ := json.Marshal(f)
	m.mu.Lock()
	m.filters["filter-1"] = fData
	m.mu.Unlock()

	result, err := c.ListFilters(context.Background())
	a.NoError(err)
	a.Contains(result, "Found 1 filters")
	a.Contains(result, "filter-1")
	a.Contains(result, "From: boss@example.com")
	a.Contains(result, "Has attachment")
	a.Contains(result, "Add labels: Label_Important")
}

func TestListFiltersEmpty(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()

	result, err := m.client().ListFilters(context.Background())
	a.NoError(err)
	a.Equal("No filters found.", result)
}

func TestSendMessage(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	result, err := c.SendMessage(context.Background(), SendMessageIn{
		To:      "recipient@example.com",
		Subject: "Test",
		Body:    "Hello world",
	})
	a.NoError(err)
	a.Contains(result, "Email sent successfully!")
	a.Contains(result, "sent-msg-1")
}

func TestSendMessageWithAttachments(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	result, err := c.SendMessage(context.Background(), SendMessageIn{
		To:      "recipient@example.com",
		Subject: "With attachment",
		Body:    "See attached",
		Attachments: []AttachmentInput{{
			Content:  base64.StdEncoding.EncodeToString([]byte("file data")),
			Filename: "test.txt",
			MimeType: "text/plain",
		}},
	})
	a.NoError(err)
	a.Contains(result, "Attachments: 1 file(s)")
}

func TestSendMessageValidation(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	_, err := c.SendMessage(context.Background(), SendMessageIn{})
	a.Error(err)
	a.Contains(err.Error(), "to is required")

	_, err = c.SendMessage(context.Background(), SendMessageIn{To: "a@b.com"})
	a.Error(err)
	a.Contains(err.Error(), "body is required")
}

func TestSendMessageWithThreadReplyHeaders(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	m.threads["thread-1"] = fixtureThread("thread-1", 2)
	m.mu.Unlock()

	result, err := c.SendMessage(context.Background(), SendMessageIn{
		To:       "recipient@example.com",
		Subject:  "Re: Thread Subject",
		Body:     "Reply body",
		ThreadID: "thread-1",
	})
	a.NoError(err)
	a.Contains(result, "Email sent successfully!")
}

func TestDraftMessage(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	result, err := c.DraftMessage(context.Background(), DraftMessageIn{
		To:      "recipient@example.com",
		Subject: "Draft Test",
		Body:    "Draft body",
	})
	a.NoError(err)
	a.Contains(result, "Draft created successfully!")
	a.Contains(result, "draft-1")
}

func TestDraftMessageWithSignature(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	result, err := c.DraftMessage(context.Background(), DraftMessageIn{
		To:               "recipient@example.com",
		Subject:          "Signed Draft",
		Body:             "Draft body",
		IncludeSignature: true,
	})
	a.NoError(err)
	a.Contains(result, "Draft created successfully!")
}

func TestDraftMessageWithQuoteOriginal(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	m.threads["thread-1"] = fixtureThread("thread-1", 2)
	m.mu.Unlock()

	result, err := c.DraftMessage(context.Background(), DraftMessageIn{
		To:            "recipient@example.com",
		Subject:       "Re: Thread Subject",
		Body:          "My reply",
		ThreadID:      "thread-1",
		QuoteOriginal: true,
	})
	a.NoError(err)
	a.Contains(result, "Draft created successfully!")
}

func TestManageLabelCreate(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	result, err := c.ManageLabel(context.Background(), ManageLabelIn{
		Action: "create",
		Name:   "Projects",
	})
	a.NoError(err)
	a.Contains(result, "Label created successfully!")
	a.Contains(result, "Projects")
}

func TestManageLabelUpdate(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	labelData, _ := json.Marshal(Label{ID: "Label_1", Name: "OldName"})
	m.mu.Lock()
	m.labels["Label_1"] = labelData
	m.mu.Unlock()

	result, err := c.ManageLabel(context.Background(), ManageLabelIn{
		Action:  "update",
		LabelID: "Label_1",
		Name:    "NewName",
	})
	r.NoError(err)
	a.Contains(result, "Label updated successfully!")
	a.Contains(result, "NewName")
}

func TestManageLabelDelete(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	labelData, _ := json.Marshal(Label{ID: "Label_1", Name: "ToDelete"})
	m.mu.Lock()
	m.labels["Label_1"] = labelData
	m.mu.Unlock()

	result, err := c.ManageLabel(context.Background(), ManageLabelIn{
		Action:  "delete",
		LabelID: "Label_1",
	})
	a.NoError(err)
	a.Contains(result, "deleted successfully!")
	a.Contains(result, "ToDelete")

	m.mu.Lock()
	_, exists := m.labels["Label_1"]
	m.mu.Unlock()
	a.False(exists)
}

func TestManageLabelValidation(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	_, err := c.ManageLabel(context.Background(), ManageLabelIn{Action: "create"})
	a.Error(err)
	a.Contains(err.Error(), "label name is required")

	_, err = c.ManageLabel(context.Background(), ManageLabelIn{Action: "update"})
	a.Error(err)
	a.Contains(err.Error(), "label_id is required")

	_, err = c.ManageLabel(context.Background(), ManageLabelIn{Action: "delete"})
	a.Error(err)
	a.Contains(err.Error(), "label_id is required")

	_, err = c.ManageLabel(context.Background(), ManageLabelIn{Action: "invalid"})
	a.Error(err)
	a.Contains(err.Error(), "invalid action")
}

func TestManageFilterCreate(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	result, err := c.ManageFilter(context.Background(), ManageFilterIn{
		Action:       "create",
		Criteria:     &FilterCriteria{From: "spam@example.com"},
		FilterAction: &FilterAction{AddLabelIDs: []string{"TRASH"}},
	})
	a.NoError(err)
	a.Contains(result, "Filter created successfully!")
}

func TestManageFilterDelete(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	f := Filter{
		ID:       "filter-1",
		Criteria: FilterCriteria{From: "spam@example.com"},
		Action:   FilterAction{AddLabelIDs: []string{"TRASH"}},
	}
	fData, _ := json.Marshal(f)
	m.mu.Lock()
	m.filters["filter-1"] = fData
	m.mu.Unlock()

	result, err := c.ManageFilter(context.Background(), ManageFilterIn{
		Action:   "delete",
		FilterID: "filter-1",
	})
	a.NoError(err)
	a.Contains(result, "Filter deleted successfully!")

	m.mu.Lock()
	_, exists := m.filters["filter-1"]
	m.mu.Unlock()
	a.False(exists)
}

func TestManageFilterValidation(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	_, err := c.ManageFilter(context.Background(), ManageFilterIn{Action: "create"})
	a.Error(err)
	a.Contains(err.Error(), "criteria and filter_action are required")

	_, err = c.ManageFilter(context.Background(), ManageFilterIn{Action: "delete"})
	a.Error(err)
	a.Contains(err.Error(), "filter_id is required")

	_, err = c.ManageFilter(context.Background(), ManageFilterIn{Action: "invalid"})
	a.Error(err)
	a.Contains(err.Error(), "invalid action")
}

func TestModifyMessageLabels(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	result, err := c.ModifyMessageLabels(context.Background(), ModifyMessageLabelsIn{
		MessageID:   "msg-1",
		AddLabelIDs: []string{"Label_1"},
	})
	a.NoError(err)
	a.Contains(result, "Labels updated for message msg-1")
	a.Contains(result, "Added labels: Label_1")
}

func TestModifyMessageLabelsValidation(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	_, err := c.ModifyMessageLabels(context.Background(), ModifyMessageLabelsIn{})
	a.Error(err)
	a.Contains(err.Error(), "message_id is required")

	_, err = c.ModifyMessageLabels(context.Background(), ModifyMessageLabelsIn{MessageID: "msg-1"})
	a.Error(err)
	a.Contains(err.Error(), "at least one of")
}

func TestBatchModifyMessageLabels(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	result, err := c.BatchModifyMessageLabels(context.Background(), BatchModifyMessageLabelsIn{
		MessageIDs:     []string{"msg-1", "msg-2", "msg-3"},
		AddLabelIDs:    []string{"Label_1"},
		RemoveLabelIDs: []string{"INBOX"},
	})
	a.NoError(err)
	a.Contains(result, "Labels updated for 3 messages")
	a.Contains(result, "Added labels: Label_1")
	a.Contains(result, "Removed labels: INBOX")

	m.mu.Lock()
	body := m.lastBatchModifyBody
	m.mu.Unlock()
	a.NotNil(body)
	ids, ok := body["ids"].([]any)
	a.True(ok)
	a.Len(ids, 3)
}

func TestBatchModifyMessageLabelsValidation(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	_, err := c.BatchModifyMessageLabels(context.Background(), BatchModifyMessageLabelsIn{})
	a.Error(err)
	a.Contains(err.Error(), "message_ids is required")

	_, err = c.BatchModifyMessageLabels(context.Background(), BatchModifyMessageLabelsIn{
		MessageIDs: []string{"msg-1"},
	})
	a.Error(err)
	a.Contains(err.Error(), "at least one of")
}

// MIME helper tests

func TestExtractHeaders(t *testing.T) {
	a := assert.New(t)
	part := &MessagePart{
		Headers: []MessageHeader{
			{Name: "Subject", Value: "Test"},
			{Name: "from", Value: "sender@test.com"},
			{Name: "X-Custom", Value: "ignored"},
		},
	}
	h := extractHeaders(part, []string{"Subject", "From"})
	a.Equal("Test", h["Subject"])
	a.Equal("sender@test.com", h["From"])
	a.Empty(h["X-Custom"])
}

func TestExtractBodies(t *testing.T) {
	a := assert.New(t)
	part := &MessagePart{
		MimeType: "multipart/alternative",
		Parts: []MessagePart{
			{
				MimeType: "text/plain",
				Body:     &MessageBody{Data: base64.URLEncoding.EncodeToString([]byte("plain text"))},
			},
			{
				MimeType: "text/html",
				Body:     &MessageBody{Data: base64.URLEncoding.EncodeToString([]byte("<b>html</b>"))},
			},
		},
	}
	text, html := extractBodies(part)
	a.Equal("plain text", text)
	a.Equal("<b>html</b>", html)
}

func TestExtractBodiesNested(t *testing.T) {
	a := assert.New(t)
	part := &MessagePart{
		MimeType: "multipart/mixed",
		Parts: []MessagePart{
			{
				MimeType: "multipart/alternative",
				Parts: []MessagePart{
					{
						MimeType: "text/plain",
						Body:     &MessageBody{Data: base64.URLEncoding.EncodeToString([]byte("nested plain"))},
					},
					{
						MimeType: "text/html",
						Body:     &MessageBody{Data: base64.URLEncoding.EncodeToString([]byte("<p>nested html</p>"))},
					},
				},
			},
		},
	}
	text, html := extractBodies(part)
	a.Equal("nested plain", text)
	a.Equal("<p>nested html</p>", html)
}

func TestExtractAttachments(t *testing.T) {
	a := assert.New(t)
	part := &MessagePart{
		MimeType: "multipart/mixed",
		Parts: []MessagePart{
			{MimeType: "text/plain", Body: &MessageBody{Data: "dGV4dA"}},
			{
				MimeType: "application/pdf",
				Filename: "doc.pdf",
				Body:     &MessageBody{AttachmentID: "att-1", Size: 500},
			},
			{
				MimeType: "image/png",
				Filename: "photo.png",
				Body:     &MessageBody{AttachmentID: "att-2", Size: 1000},
			},
		},
	}
	atts := extractAttachments(part)
	a.Len(atts, 2)
	a.Equal("doc.pdf", atts[0].Filename)
	a.Equal("photo.png", atts[1].Filename)
}

func TestHtmlToText(t *testing.T) {
	a := assert.New(t)

	a.Equal("Hello world", htmlToText("<p>Hello <b>world</b></p>"))
	a.Equal("Visible text", htmlToText("<div>Visible text<script>var x=1;</script></div>"))
	a.Equal("BeforeAfter", htmlToText("Before<style>.x{color:red}</style>After"))
	a.Equal("", htmlToText(""))
}

func TestFormatBodyContent(t *testing.T) {
	a := assert.New(t)

	a.Equal("plain text", formatBodyContent("plain text", ""))
	a.Equal("[No readable content found]", formatBodyContent("", ""))

	result := formatBodyContent("", "<b>html only</b>")
	a.Equal("html only", result)

	a.Equal("good plain", formatBodyContent("good plain", "<b>html</b>"))

	result = formatBodyContent("<!-- comment -->", "<b>real content</b>")
	a.Equal("real content", result)
}

func TestFormatBodyContentLowValue(t *testing.T) {
	a := assert.New(t)
	plain := "view this email in your browser"
	html := "<div>Actual rich content with images and links</div>"
	result := formatBodyContent(plain, html)
	a.Equal("Actual rich content with images and links", result)
}

func TestFormatBodyContentTruncation(t *testing.T) {
	a := assert.New(t)
	longHTML := "<div>" + strings.Repeat("x", htmlBodyTruncateLimit+100) + "</div>"
	result := formatBodyContent("", longHTML)
	a.Contains(result, "[Content truncated...]")
	a.True(len(result) < htmlBodyTruncateLimit+100)
}

func TestBuildRawMessage(t *testing.T) {
	a := assert.New(t)

	raw, count, err := buildRawMessage(buildMessageParams{
		To:      "recipient@example.com",
		Subject: "Test",
		Body:    "Hello",
	})
	a.NoError(err)
	a.Equal(0, count)

	decoded, err := base64.URLEncoding.DecodeString(raw)
	a.NoError(err)
	content := string(decoded)
	a.Contains(content, "To: recipient@example.com")
	a.Contains(content, "Subject: Test")
	a.Contains(content, "Hello")
}

func TestBuildRawMessageWithAttachments(t *testing.T) {
	a := assert.New(t)

	raw, count, err := buildRawMessage(buildMessageParams{
		To:      "recipient@example.com",
		Subject: "With Attachment",
		Body:    "See attached",
		Attachments: []AttachmentInput{{
			Content:  base64.StdEncoding.EncodeToString([]byte("data")),
			Filename: "test.txt",
			MimeType: "text/plain",
		}},
	})
	a.NoError(err)
	a.Equal(1, count)

	decoded, err := base64.URLEncoding.DecodeString(raw)
	a.NoError(err)
	content := string(decoded)
	a.Contains(content, "multipart/mixed")
	a.Contains(content, "test.txt")
}

func TestEncodeSubject(t *testing.T) {
	a := assert.New(t)

	a.Equal("Hello World", encodeSubject("Hello World"))
	encoded := encodeSubject("Bonjour le monde!")
	a.Equal("Bonjour le monde!", encoded)

	encoded = encodeSubject("Héllo Wörld")
	a.Contains(encoded, "=?UTF-8?")
}

func TestSanitizeHeader(t *testing.T) {
	a := assert.New(t)
	a.Equal("safe header", sanitizeHeader("safe header"))
	a.Equal("nonewlines", sanitizeHeader("no\r\nnewlines"))
}

func TestDeriveReplyHeaders(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	m.mu.Lock()
	m.threads["thread-1"] = fixtureThread("thread-1", 3)
	m.mu.Unlock()

	inReplyTo, references, err := c.deriveReplyHeaders(context.Background(), "thread-1")
	a.NoError(err)
	a.Contains(inReplyTo, "@example.com>")
	a.Contains(references, "@example.com>")
}

func TestGetSendAsSignatureHTML(t *testing.T) {
	a := assert.New(t)
	m := newMockGmailServer()
	defer m.close()
	c := m.client()

	sig := c.getSendAsSignatureHTML(context.Background(), "")
	a.Equal("<div>-- Test Signature</div>", sig)
}

func TestAppendSignatureToBody(t *testing.T) {
	a := assert.New(t)

	result := appendSignatureToBody("<p>Body</p>", "html", "<div>Sig</div>")
	a.Equal("<p>Body</p><br><br><div>Sig</div>", result)

	result = appendSignatureToBody("Plain body", "plain", "<div>Sig</div>")
	a.Contains(result, "<div>Plain body</div>")
	a.Contains(result, "<div>Sig</div>")
}

func TestBuildQuotedReplyBody(t *testing.T) {
	a := assert.New(t)

	result := buildQuotedReplyBody("My reply", "plain", &originalMessage{
		Body: "Original text",
		Date: "Mon, 1 Jan 2024",
		From: "sender@example.com",
	})
	a.Contains(result, "My reply")
	a.Contains(result, "gmail_quote")
	a.Contains(result, "sender@example.com wrote:")
	a.Contains(result, "Original text")
}
