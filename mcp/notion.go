package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/cockroachdb/errors"
)

const notionAPIBase = "https://api.notion.com/v1"
const notionAPIVersion = "2022-06-28"

type NotionClient struct {
	Token   string
	BaseURL string
}

type NotionBlock struct {
	Content        json.RawMessage `json:"content"`
	CreatedTime    string          `json:"created_time"`
	HasChildren    bool            `json:"has_children"`
	ID             string          `json:"id"`
	LastEditedTime string          `json:"last_edited_time"`
	Type           string          `json:"type"`
}

type NotionDatabase struct {
	Archived       bool   `json:"archived"`
	CreatedTime    string `json:"created_time"`
	Description    string `json:"description"`
	ID             string `json:"id"`
	LastEditedTime string `json:"last_edited_time"`
	Title          string `json:"title"`
	URL            string `json:"url"`
}

type NotionPage struct {
	Archived       bool            `json:"archived"`
	CreatedTime    string          `json:"created_time"`
	ID             string          `json:"id"`
	LastEditedTime string          `json:"last_edited_time"`
	Properties     json.RawMessage `json:"properties"`
	URL            string          `json:"url"`
}

type AppendBlocksOut struct {
	Results json.RawMessage `json:"results"`
}

type CreatePageOut struct {
	NotionPage
}

type UpdatePageOut struct {
	NotionPage
}

type GetDatabaseOut struct {
	NotionDatabase
	Properties json.RawMessage `json:"properties,omitempty"`
}

type GetPageContentOut struct {
	Blocks     []NotionBlock `json:"blocks"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type GetPageOut struct {
	NotionPage
}

type QueryDatabaseOut struct {
	HasMore    bool              `json:"has_more"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Results    []json.RawMessage `json:"results"`
}

type SearchOut struct {
	HasMore    bool              `json:"has_more"`
	NextCursor string            `json:"next_cursor,omitempty"`
	Results    []json.RawMessage `json:"results"`
}

func (c *NotionClient) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (json.RawMessage, error) {
	base := c.BaseURL
	if base == "" {
		base = notionAPIBase
	}
	apiURL := base + path
	if len(query) > 0 {
		apiURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Notion-Version", notionAPIVersion)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "notion api request")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Message != "" {
			return nil, errors.Newf("notion api error (%d): %s", resp.StatusCode, apiErr.Message)
		}
		return nil, errors.Newf("notion api error (%d): %s", resp.StatusCode, string(data))
	}

	return json.RawMessage(data), nil
}

func (c *NotionClient) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, "")
}

func (c *NotionClient) patch(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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
	return c.do(ctx, http.MethodPatch, path, query, body, ct)
}

func (c *NotionClient) post(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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

func (c *NotionClient) AppendBlocks(ctx context.Context, blockID string, texts []string) (AppendBlocksOut, error) {
	var out AppendBlocksOut

	children := make([]map[string]any, 0, len(texts))
	for _, text := range texts {
		children = append(children, map[string]any{
			"object": "block",
			"type":   "paragraph",
			"paragraph": map[string]any{
				"rich_text": []map[string]any{
					{
						"type": "text",
						"text": map[string]any{
							"content": text,
						},
					},
				},
			},
		})
	}

	payload := map[string]any{
		"children": children,
	}

	data, err := c.patch(ctx, "/blocks/"+url.PathEscape(blockID)+"/children", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "append blocks")
	}

	var resp struct {
		Results json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode append blocks")
	}
	out.Results = resp.Results
	return out, nil
}

func (c *NotionClient) detectTitleProperty(ctx context.Context, databaseID string) string {
	data, err := c.get(ctx, "/databases/"+url.PathEscape(databaseID), nil)
	if err != nil {
		return "title"
	}
	var db struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &db); err != nil {
		return "title"
	}
	for name, prop := range db.Properties {
		if prop.Type == "title" {
			return name
		}
	}
	return "title"
}

func (c *NotionClient) CreatePage(ctx context.Context, parentPageID, parentDatabaseID, title, content string, extraProperties json.RawMessage) (CreatePageOut, error) {
	var out CreatePageOut

	parent := map[string]any{}
	titleProp := "title"
	if parentDatabaseID != "" {
		parent["database_id"] = parentDatabaseID
		titleProp = c.detectTitleProperty(ctx, parentDatabaseID)
	} else if parentPageID != "" {
		parent["page_id"] = parentPageID
	} else {
		return out, errors.Newf("either parentPageID or parentDatabaseID is required")
	}

	properties := map[string]any{}
	if len(extraProperties) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(extraProperties, &parsed); err != nil {
			return out, errors.Wrap(err, "parse extra properties")
		}
		for k, v := range parsed {
			properties[k] = v
		}
	}

	if title != "" {
		properties[titleProp] = map[string]any{
			"title": []map[string]any{
				{
					"type": "text",
					"text": map[string]any{
						"content": title,
					},
				},
			},
		}
	}

	payload := map[string]any{
		"parent":     parent,
		"properties": properties,
	}

	if content != "" {
		payload["children"] = []map[string]any{
			{
				"object": "block",
				"type":   "paragraph",
				"paragraph": map[string]any{
					"rich_text": []map[string]any{
						{
							"type": "text",
							"text": map[string]any{
								"content": content,
							},
						},
					},
				},
			},
		}
	}

	data, err := c.post(ctx, "/pages", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "create page")
	}

	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode created page")
	}

	return out, nil
}

func (c *NotionClient) UpdatePage(ctx context.Context, pageID string, properties json.RawMessage) (UpdatePageOut, error) {
	var out UpdatePageOut

	if pageID == "" {
		return out, errors.Newf("pageID is required")
	}

	parsed := map[string]any{}
	if len(properties) > 0 {
		if err := json.Unmarshal(properties, &parsed); err != nil {
			return out, errors.Wrap(err, "parse properties")
		}
	}

	payload := map[string]any{
		"properties": parsed,
	}

	data, err := c.patch(ctx, "/pages/"+url.PathEscape(pageID), nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "update page")
	}

	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode updated page")
	}

	return out, nil
}

func (c *NotionClient) GetDatabase(ctx context.Context, databaseID string) (GetDatabaseOut, error) {
	var out GetDatabaseOut

	data, err := c.get(ctx, "/databases/"+url.PathEscape(databaseID), nil)
	if err != nil {
		return out, errors.Wrap(err, "get database")
	}

	var raw struct {
		Archived    bool   `json:"archived"`
		CreatedTime string `json:"created_time"`
		Description []struct {
			PlainText string `json:"plain_text"`
		} `json:"description"`
		ID             string          `json:"id"`
		LastEditedTime string          `json:"last_edited_time"`
		Properties     json.RawMessage `json:"properties"`
		Title          []struct {
			PlainText string `json:"plain_text"`
		} `json:"title"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return out, errors.Wrap(err, "decode database")
	}

	out.Archived = raw.Archived
	out.CreatedTime = raw.CreatedTime
	out.ID = raw.ID
	out.LastEditedTime = raw.LastEditedTime
	out.Properties = raw.Properties
	out.URL = raw.URL

	for _, t := range raw.Title {
		out.Title += t.PlainText
	}
	for _, d := range raw.Description {
		out.Description += d.PlainText
	}

	return out, nil
}

func (c *NotionClient) GetPage(ctx context.Context, pageID string) (GetPageOut, error) {
	var out GetPageOut

	data, err := c.get(ctx, "/pages/"+url.PathEscape(pageID), nil)
	if err != nil {
		return out, errors.Wrap(err, "get page")
	}

	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode page")
	}

	return out, nil
}

func (c *NotionClient) GetPageContent(ctx context.Context, pageID string) (GetPageContentOut, error) {
	var out GetPageContentOut

	params := url.Values{
		"page_size": {"100"},
	}

	data, err := c.get(ctx, "/blocks/"+url.PathEscape(pageID)+"/children", params)
	if err != nil {
		return out, errors.Wrap(err, "get page content")
	}

	var resp struct {
		HasMore    bool   `json:"has_more"`
		NextCursor string `json:"next_cursor"`
		Results    []struct {
			CreatedTime    string `json:"created_time"`
			HasChildren    bool   `json:"has_children"`
			ID             string `json:"id"`
			LastEditedTime string `json:"last_edited_time"`
			Type           string `json:"type"`
		}
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode page content")
	}

	var resultsWrapper struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(data, &resultsWrapper); err != nil {
		return out, errors.Wrap(err, "decode page content results")
	}

	out.HasMore = resp.HasMore
	out.NextCursor = resp.NextCursor
	out.Blocks = make([]NotionBlock, len(resp.Results))
	for i, r := range resp.Results {
		out.Blocks[i] = NotionBlock{
			CreatedTime:    r.CreatedTime,
			HasChildren:    r.HasChildren,
			ID:             r.ID,
			LastEditedTime: r.LastEditedTime,
			Type:           r.Type,
		}
		if i < len(resultsWrapper.Results) {
			var blockMap map[string]json.RawMessage
			if err := json.Unmarshal(resultsWrapper.Results[i], &blockMap); err == nil {
				if typeContent, ok := blockMap[r.Type]; ok {
					out.Blocks[i].Content = typeContent
				}
			}
		}
	}

	return out, nil
}

func (c *NotionClient) QueryDatabase(ctx context.Context, databaseID string, pageSize int, startCursor string) (QueryDatabaseOut, error) {
	var out QueryDatabaseOut
	if pageSize <= 0 {
		pageSize = 10
	}

	payload := map[string]any{
		"page_size": pageSize,
	}
	if startCursor != "" {
		payload["start_cursor"] = startCursor
	}

	data, err := c.post(ctx, "/databases/"+url.PathEscape(databaseID)+"/query", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "query database")
	}

	var resp struct {
		HasMore    bool              `json:"has_more"`
		NextCursor string            `json:"next_cursor"`
		Results    []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode query database")
	}

	out.HasMore = resp.HasMore
	out.NextCursor = resp.NextCursor
	out.Results = resp.Results
	return out, nil
}

func (c *NotionClient) Search(ctx context.Context, query string, pageSize int, startCursor string, filterValue string) (SearchOut, error) {
	var out SearchOut
	if pageSize <= 0 {
		pageSize = 10
	}

	payload := map[string]any{
		"page_size": pageSize,
	}
	if query != "" {
		payload["query"] = query
	}
	if startCursor != "" {
		payload["start_cursor"] = startCursor
	}
	if filterValue != "" {
		payload["filter"] = map[string]any{
			"property": "object",
			"value":    filterValue,
		}
	}

	data, err := c.post(ctx, "/search", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "search")
	}

	var resp struct {
		HasMore    bool              `json:"has_more"`
		NextCursor string            `json:"next_cursor"`
		Results    []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode search")
	}

	out.HasMore = resp.HasMore
	out.NextCursor = resp.NextCursor
	out.Results = resp.Results
	return out, nil
}
