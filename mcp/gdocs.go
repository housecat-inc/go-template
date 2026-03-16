package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cockroachdb/errors"
)

const gdocsAPIBase = "https://docs.googleapis.com/v1"

type GDocsClient struct {
	Token string
}

type GDocsDocument struct {
	DocumentID string `json:"documentId"`
	Title      string `json:"title"`
	RevisionID string `json:"revisionId,omitempty"`
}

func (c *GDocsClient) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (json.RawMessage, error) {
	apiURL := gdocsAPIBase + path
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
		return nil, errors.Wrap(err, "docs api request")
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
			return nil, errors.Newf("docs api error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, errors.Newf("docs api error (%d): %s", resp.StatusCode, string(data))
	}

	return json.RawMessage(data), nil
}

func (c *GDocsClient) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, "")
}

func (c *GDocsClient) post(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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

type GetDocumentOut struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
	Body       string `json:"body"`
}

func (c *GDocsClient) GetDocument(ctx context.Context, documentID string) (GetDocumentOut, error) {
	var out GetDocumentOut

	data, err := c.get(ctx, "/documents/"+url.PathEscape(documentID), nil)
	if err != nil {
		return out, errors.Wrap(err, "get document")
	}

	var doc struct {
		DocumentID string `json:"documentId"`
		Title      string `json:"title"`
		Body       struct {
			Content []struct {
				Paragraph *struct {
					Elements []struct {
						TextRun *struct {
							Content string `json:"content"`
						} `json:"textRun,omitempty"`
					} `json:"elements"`
				} `json:"paragraph,omitempty"`
			} `json:"content"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, errors.Wrap(err, "decode document")
	}

	out.DocumentID = doc.DocumentID
	out.Title = doc.Title

	var sb strings.Builder
	for _, block := range doc.Body.Content {
		if block.Paragraph != nil {
			for _, elem := range block.Paragraph.Elements {
				if elem.TextRun != nil {
					sb.WriteString(elem.TextRun.Content)
				}
			}
		}
	}
	out.Body = sb.String()

	return out, nil
}

type CreateDocumentOut struct {
	DocumentID string `json:"document_id"`
	Title      string `json:"title"`
}

func (c *GDocsClient) CreateDocument(ctx context.Context, title string) (CreateDocumentOut, error) {
	var out CreateDocumentOut

	payload := map[string]any{"title": title}
	data, err := c.post(ctx, "/documents", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "create document")
	}

	var doc GDocsDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return out, errors.Wrap(err, "decode document")
	}

	out.DocumentID = doc.DocumentID
	out.Title = doc.Title
	return out, nil
}

type BatchUpdateDocumentOut struct {
	DocumentID string `json:"document_id"`
}

func (c *GDocsClient) BatchUpdate(ctx context.Context, documentID string, requests []map[string]any) (BatchUpdateDocumentOut, error) {
	var out BatchUpdateDocumentOut

	payload := map[string]any{"requests": requests}
	_, err := c.post(ctx, "/documents/"+url.PathEscape(documentID)+":batchUpdate", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "batch update document")
	}

	out.DocumentID = documentID
	return out, nil
}

func (c *GDocsClient) InsertText(ctx context.Context, documentID, text string, index int) (BatchUpdateDocumentOut, error) {
	if index <= 0 {
		index = 1
	}
	requests := []map[string]any{
		{
			"insertText": map[string]any{
				"location": map[string]any{"index": index},
				"text":     text,
			},
		},
	}
	return c.BatchUpdate(ctx, documentID, requests)
}
