package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cockroachdb/errors"
)

const granolaAPIBase = "https://api.granola.ai/v2"

type GranolaClient struct {
	Token string
}

type GranolaDocument struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
	Notes     string `json:"notes"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
}

type GranolaDocumentDetail struct {
	CreatedAt  string `json:"created_at"`
	ID         string `json:"id"`
	Notes      string `json:"notes"`
	Title      string `json:"title"`
	Transcript string `json:"transcript"`
	UpdatedAt  string `json:"updated_at"`
}

func (c *GranolaClient) do(ctx context.Context, method, path string, query url.Values) (json.RawMessage, error) {
	apiURL := granolaAPIBase + path
	if len(query) > 0 {
		apiURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, apiURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "granola api request")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}
	if resp.StatusCode >= 400 {
		return nil, errors.Newf("granola api error (%d): %s", resp.StatusCode, string(data))
	}
	return json.RawMessage(data), nil
}

func (c *GranolaClient) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query)
}

type ListDocumentsOut struct {
	Documents []GranolaDocument `json:"documents"`
}

const maxListDocuments = 25

func (c *GranolaClient) ListDocuments(ctx context.Context, limit int) (ListDocumentsOut, error) {
	var out ListDocumentsOut
	if limit <= 0 {
		limit = 10
	}
	if limit > maxListDocuments {
		limit = maxListDocuments
	}
	params := url.Values{
		"limit": {fmt.Sprintf("%d", limit)},
	}
	data, err := c.get(ctx, "/documents", params)
	if err != nil {
		return out, errors.Wrap(err, "list documents")
	}
	var resp []GranolaDocument
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode documents")
	}
	out.Documents = resp
	return out, nil
}

type GetDocumentOut = GranolaDocumentDetail

func (c *GranolaClient) GetDocument(ctx context.Context, id string) (GetDocumentOut, error) {
	var out GetDocumentOut
	data, err := c.get(ctx, "/documents/"+url.PathEscape(id), nil)
	if err != nil {
		return out, errors.Wrap(err, "get document")
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode document")
	}
	return out, nil
}

type SearchDocumentsOut struct {
	Documents []GranolaDocument `json:"documents"`
}

func (c *GranolaClient) SearchDocuments(ctx context.Context, query string, limit int) (SearchDocumentsOut, error) {
	var out SearchDocumentsOut
	if limit <= 0 {
		limit = 10
	}
	if limit > maxListDocuments {
		limit = maxListDocuments
	}
	params := url.Values{
		"limit": {fmt.Sprintf("%d", limit)},
		"q":     {query},
	}
	data, err := c.get(ctx, "/documents", params)
	if err != nil {
		return out, errors.Wrap(err, "search documents")
	}
	var resp []GranolaDocument
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode search results")
	}
	out.Documents = resp
	return out, nil
}
