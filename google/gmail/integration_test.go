package gmail

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GMAIL_TEST_TOKEN: read-scope token (gmail.readonly)
// GMAIL_TEST_TOKEN_WRITE: write-scope token (gmail.modify + gmail.send + gmail.labels)
// GMAIL_TEST_TOKEN_DRAFT: draft-scope token (gmail.compose)

func integrationClient(t *testing.T) *Client {
	t.Helper()
	token := os.Getenv("GMAIL_TEST_TOKEN")
	if token == "" {
		t.Skip("GMAIL_TEST_TOKEN not set")
	}
	return &Client{Token: token}
}

func integrationWriteClient(t *testing.T) *Client {
	t.Helper()
	token := os.Getenv("GMAIL_TEST_TOKEN_WRITE")
	if token == "" {
		t.Skip("GMAIL_TEST_TOKEN_WRITE not set")
	}
	return &Client{Token: token}
}

func integrationDraftClient(t *testing.T) *Client {
	t.Helper()
	token := os.Getenv("GMAIL_TEST_TOKEN_DRAFT")
	if token == "" {
		t.Skip("GMAIL_TEST_TOKEN_DRAFT not set")
	}
	return &Client{Token: token}
}

// Read operations

func TestIntegrationSearchMessages(t *testing.T) {
	c := integrationClient(t)

	result, err := c.SearchMessages(context.Background(), SearchMessagesIn{
		Query:    "in:inbox",
		PageSize: 3,
	})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "messages")
}

func TestIntegrationGetMessageContent(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	search, err := c.SearchMessages(ctx, SearchMessagesIn{Query: "in:inbox", PageSize: 1})
	require.NoError(t, err)

	msgID := extractFirstMessageID(search)
	if msgID == "" {
		t.Skip("No messages in inbox")
	}

	result, err := c.GetMessageContent(ctx, GetMessageContentIn{MessageID: msgID})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "Message ID:")
	assert.Contains(t, result, "Subject:")
	assert.Contains(t, result, "From:")
}

func TestIntegrationGetMessagesContentBatch(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	search, err := c.SearchMessages(ctx, SearchMessagesIn{Query: "in:inbox", PageSize: 3})
	require.NoError(t, err)

	ids := extractMessageIDs(search, 3)
	if len(ids) == 0 {
		t.Skip("No messages to batch fetch")
	}

	result, err := c.GetMessagesContentBatch(ctx, GetMessagesContentBatchIn{MessageIDs: ids})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "Retrieved")
}

func TestIntegrationGetThreadContent(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	search, err := c.SearchMessages(ctx, SearchMessagesIn{Query: "in:inbox", PageSize: 1})
	require.NoError(t, err)

	threadID := extractFirstThreadID(search)
	if threadID == "" {
		t.Skip("No thread ID found")
	}

	result, err := c.GetThreadContent(ctx, GetThreadContentIn{ThreadID: threadID})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "Thread ID:")
}

func TestIntegrationGetThreadsContentBatch(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	search, err := c.SearchMessages(ctx, SearchMessagesIn{Query: "in:inbox", PageSize: 3})
	require.NoError(t, err)

	var threadIDs []string
	seen := map[string]bool{}
	for _, line := range strings.Split(search, "\n") {
		if strings.HasPrefix(line, "Thread ID: ") {
			tid := strings.TrimPrefix(line, "Thread ID: ")
			if !seen[tid] {
				seen[tid] = true
				threadIDs = append(threadIDs, tid)
			}
		}
	}
	if len(threadIDs) == 0 {
		t.Skip("No threads to batch fetch")
	}

	result, err := c.GetThreadsContentBatch(ctx, GetThreadsContentBatchIn{ThreadIDs: threadIDs})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "Retrieved")
}

func TestIntegrationListLabels(t *testing.T) {
	c := integrationClient(t)

	result, err := c.ListLabels(context.Background())
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "INBOX")
}

func TestIntegrationListFilters(t *testing.T) {
	// Requires gmail.settings.basic scope (not in standard read level).
	c := integrationClient(t)

	result, err := c.ListFilters(context.Background())
	if err != nil && strings.Contains(err.Error(), "403") {
		t.Skip("gmail.settings.basic scope not available")
	}
	require.NoError(t, err)
	t.Log(result)
}

// Write operations

func TestIntegrationManageLabelCreateAndDelete(t *testing.T) {
	c := integrationWriteClient(t)
	ctx := context.Background()

	result, err := c.ManageLabel(ctx, ManageLabelIn{
		Action: "create",
		Name:   "IntegrationTestLabel",
	})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "Label created successfully!")

	labelID := ""
	for _, line := range strings.Split(result, "\n") {
		if strings.HasPrefix(line, "ID: ") {
			labelID = strings.TrimPrefix(line, "ID: ")
		}
	}
	require.NotEmpty(t, labelID, "should have extracted label ID")

	result, err = c.ManageLabel(ctx, ManageLabelIn{
		Action:  "delete",
		LabelID: labelID,
	})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "deleted successfully!")
}

func TestIntegrationModifyMessageLabels(t *testing.T) {
	c := integrationWriteClient(t)
	ctx := context.Background()

	search, err := c.SearchMessages(ctx, SearchMessagesIn{Query: "in:inbox", PageSize: 1})
	require.NoError(t, err)

	msgID := extractFirstMessageID(search)
	if msgID == "" {
		t.Skip("No messages to modify")
	}

	result, err := c.ModifyMessageLabels(ctx, ModifyMessageLabelsIn{
		MessageID:   msgID,
		AddLabelIDs: []string{"STARRED"},
	})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "Labels updated")

	result, err = c.ModifyMessageLabels(ctx, ModifyMessageLabelsIn{
		MessageID:      msgID,
		RemoveLabelIDs: []string{"STARRED"},
	})
	require.NoError(t, err)
}

// Draft operations

func TestIntegrationDraftCreateAndSend(t *testing.T) {
	c := integrationDraftClient(t)
	ctx := context.Background()

	result, err := c.DraftMessage(ctx, DraftMessageIn{
		To:      "imogen@alleycat.biz",
		Subject: "Integration Test Draft",
		Body:    "This is an automated integration test draft.",
	})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "Draft created successfully!")
}

func TestIntegrationDraftWithSignature(t *testing.T) {
	c := integrationDraftClient(t)
	ctx := context.Background()

	result, err := c.DraftMessage(ctx, DraftMessageIn{
		To:               "imogen@alleycat.biz",
		Subject:          "Integration Test Draft With Signature",
		Body:             "Testing signature injection.",
		IncludeSignature: true,
	})
	require.NoError(t, err)
	t.Log(result)
	assert.Contains(t, result, "Draft created successfully!")
}

// Helpers

func extractFirstMessageID(search string) string {
	for _, line := range strings.Split(search, "\n") {
		if strings.HasPrefix(line, "Message ID: ") {
			return strings.TrimPrefix(line, "Message ID: ")
		}
	}
	return ""
}

func extractFirstThreadID(search string) string {
	for _, line := range strings.Split(search, "\n") {
		if strings.HasPrefix(line, "Thread ID: ") {
			return strings.TrimPrefix(line, "Thread ID: ")
		}
	}
	return ""
}

func extractMessageIDs(search string, max int) []string {
	var ids []string
	for _, line := range strings.Split(search, "\n") {
		if strings.HasPrefix(line, "Message ID: ") {
			ids = append(ids, strings.TrimPrefix(line, "Message ID: "))
			if len(ids) >= max {
				break
			}
		}
	}
	return ids
}
