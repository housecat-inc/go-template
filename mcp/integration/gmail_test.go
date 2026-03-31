package integration

import (
	"testing"
)

func TestGmailToolsByLevel(t *testing.T) {
	db, q := openDB(t)
	token := tokenForSubject(t, db, q, imogenSubject, "gmail", "read")

	tests := []toolTest{
		// --- read level ---
		{"read/search_messages succeeds", "gmail_search_messages", `{"query":"in:inbox","page_size":3}`, []string{"read"}, ""},
		{"read/list_labels succeeds", "gmail_list_labels", `{}`, []string{"read"}, ""},
		{"read/draft_message rejected", "gmail_draft_message", `{"to":"test@test.com","subject":"test","body":"test"}`, []string{"read"}, "draft level not connected"},
		{"read/manage_label rejected", "gmail_manage_label", `{"action":"create","name":"Test"}`, []string{"read"}, "write level not connected"},
		{"read/send_message rejected", "gmail_send_message", `{"to":"test@test.com","subject":"test","body":"test"}`, []string{"read"}, "write level not connected"},
		{"read/trash_message rejected", "gmail_trash_message", `{"message_id":"abc"}`, []string{"read"}, "archive level not connected"},

		// --- draft level ---
		{"draft/search_messages succeeds via fallback", "gmail_search_messages", `{"query":"in:inbox","page_size":3}`, []string{"draft"}, ""},
		{"draft/draft_message succeeds", "gmail_draft_message", `{"to":"imogen@alleycat.biz","subject":"Integration Test Draft","body":"Automated integration test draft."}`, []string{"draft"}, ""},
		{"draft/manage_label rejected", "gmail_manage_label", `{"action":"create","name":"Test"}`, []string{"draft"}, "write level not connected"},
		{"draft/trash_message rejected", "gmail_trash_message", `{"message_id":"abc"}`, []string{"draft"}, "archive level not connected"},

		// --- write level ---
		{"write/search_messages succeeds via fallback", "gmail_search_messages", `{"query":"in:inbox","page_size":3}`, []string{"write"}, ""},
		{"write/draft_message succeeds via fallback", "gmail_draft_message", `{"to":"imogen@alleycat.biz","subject":"Integration Test Draft","body":"Automated integration test draft."}`, []string{"write"}, ""},
		{"write/manage_label succeeds", "gmail_manage_label", `{"action":"create","name":"IntegrationTestLabel"}`, []string{"write"}, ""},
		{"write/trash_message rejected", "gmail_trash_message", `{"message_id":"abc"}`, []string{"write"}, "archive level not connected"},

		// --- archive level alone ---
		{"archive only/search_messages rejected", "gmail_search_messages", `{"query":"in:inbox"}`, []string{"archive"}, "read level not connected"},
		{"archive only/draft_message rejected", "gmail_draft_message", `{"to":"test@test.com","subject":"test","body":"test"}`, []string{"archive"}, "draft level not connected"},
		{"archive only/trash_message succeeds", "gmail_trash_message", `{"message_id":"abc"}`, []string{"archive"}, ""},
	}

	runToolTests(t, "gmail", token, tests)
}
