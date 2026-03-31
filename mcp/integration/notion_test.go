package integration

import (
	"testing"
)

func TestNotionToolsByLevel(t *testing.T) {
	db, q := openDB(t)
	token := tokenForSubject(t, db, q, imogenSubject, "notion", "read")

	tests := []toolTest{
		// --- read level ---
		{"read/search succeeds", "notion-search", `{"query":"test","query_type":"internal"}`, []string{"read"}, ""},
		{"read/get-teams succeeds", "notion-get-teams", `{}`, []string{"read"}, ""},
		{"read/get-users succeeds", "notion-get-users", `{}`, []string{"read"}, ""},
		{"read/create-pages rejected", "notion-create-pages", `{"pages":[{"properties":{"title":"Test"}}]}`, []string{"read"}, "draft level not connected"},
		{"read/update-page rejected", "notion-update-page", `{"page_id":"abc","command":"update_properties"}`, []string{"read"}, "write level not connected"},
		{"read/create-comment rejected", "notion-create-comment", `{"page_id":"abc","rich_text":[{"text":{"content":"test"}}]}`, []string{"read"}, "write level not connected"},

		// --- draft level ---
		{"draft/search succeeds via fallback", "notion-search", `{"query":"test","query_type":"internal"}`, []string{"draft"}, ""},
		{"draft/create-pages private succeeds", "notion-create-pages", `{"pages":[{"properties":{"title":"Draft Level Test"},"content":"Integration test. Safe to delete."}]}`, []string{"draft"}, ""},
		{"draft/create-pages with parent rejected", "notion-create-pages", `{"parent":{"page_id":"abc"},"pages":[{"properties":{"title":"Test"}}]}`, []string{"draft"}, "draft level only allows creating private pages"},
		{"draft/create-database with parent rejected", "notion-create-database", `{"parent":{"page_id":"abc"},"schema":"CREATE TABLE (\"Name\" TITLE)"}`, []string{"draft"}, "draft level only allows creating private pages"},
		{"draft/update-page rejected", "notion-update-page", `{"page_id":"abc","command":"update_properties"}`, []string{"draft"}, "write level not connected"},
		{"draft/move-pages rejected", "notion-move-pages", `{"page_or_database_ids":["abc"],"new_parent":{"type":"workspace"}}`, []string{"draft"}, "write level not connected"},

		// --- write level ---
		{"write/search succeeds via fallback", "notion-search", `{"query":"test","query_type":"internal"}`, []string{"write"}, ""},
		{"write/create-pages private succeeds via fallback", "notion-create-pages", `{"pages":[{"properties":{"title":"Write Level Test"},"content":"Integration test. Safe to delete."}]}`, []string{"write"}, ""},
		{"write/update-data-source in_trash rejected without archive", "notion-update-data-source", `{"data_source_id":"abc","in_trash":true}`, []string{"write"}, "archive or draft level required"},

		// --- archive level alone ---
		{"archive only/search rejected", "notion-search", `{"query":"test"}`, []string{"archive"}, "read level not connected"},
		{"archive only/create-pages rejected", "notion-create-pages", `{"pages":[{"properties":{"title":"Test"}}]}`, []string{"archive"}, "draft level not connected"},
	}

	runToolTests(t, "notion", token, tests)
}
