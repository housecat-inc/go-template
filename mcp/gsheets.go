package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cockroachdb/errors"
)

const gsheetsAPIBase = "https://sheets.googleapis.com/v4"

type GSheetsClient struct {
	Token string
}

type GSheet struct {
	Properties struct {
		SheetID int    `json:"sheetId"`
		Title   string `json:"title"`
	} `json:"properties"`
}

type GSpreadsheet struct {
	SpreadsheetID string `json:"spreadsheetId"`
	Title         string `json:"title"`
	Sheets        []struct {
		Properties struct {
			SheetID int    `json:"sheetId"`
			Title   string `json:"title"`
		} `json:"properties"`
	} `json:"sheets"`
}

func (c *GSheetsClient) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (json.RawMessage, error) {
	apiURL := gsheetsAPIBase + path
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
		return nil, errors.Wrap(err, "sheets api request")
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
			return nil, errors.Newf("sheets api error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, errors.Newf("sheets api error (%d): %s", resp.StatusCode, string(data))
	}

	return json.RawMessage(data), nil
}

func (c *GSheetsClient) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, "")
}

func (c *GSheetsClient) post(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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

func (c *GSheetsClient) put(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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

type SheetInfo struct {
	SheetID int    `json:"sheet_id"`
	Title   string `json:"title"`
}

type GetSpreadsheetOut struct {
	SpreadsheetID string      `json:"spreadsheet_id"`
	Title         string      `json:"title"`
	Sheets        []SheetInfo `json:"sheets"`
}

func (c *GSheetsClient) GetSpreadsheet(ctx context.Context, spreadsheetID string) (GetSpreadsheetOut, error) {
	var out GetSpreadsheetOut

	data, err := c.get(ctx, "/spreadsheets/"+url.PathEscape(spreadsheetID), nil)
	if err != nil {
		return out, errors.Wrap(err, "get spreadsheet")
	}

	var ss GSpreadsheet
	if err := json.Unmarshal(data, &ss); err != nil {
		return out, errors.Wrap(err, "decode spreadsheet")
	}

	out.SpreadsheetID = ss.SpreadsheetID
	out.Title = ss.Title
	for _, s := range ss.Sheets {
		out.Sheets = append(out.Sheets, SheetInfo{
			SheetID: s.Properties.SheetID,
			Title:   s.Properties.Title,
		})
	}
	return out, nil
}

type GetValuesOut struct {
	Range  string     `json:"range"`
	Values [][]string `json:"values"`
}

func (c *GSheetsClient) GetValues(ctx context.Context, spreadsheetID, rangeA1 string) (GetValuesOut, error) {
	var out GetValuesOut

	path := fmt.Sprintf("/spreadsheets/%s/values/%s", url.PathEscape(spreadsheetID), url.PathEscape(rangeA1))
	data, err := c.get(ctx, path, nil)
	if err != nil {
		return out, errors.Wrap(err, "get values")
	}

	var resp struct {
		Range  string          `json:"range"`
		Values [][]interface{} `json:"values"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode values")
	}

	out.Range = resp.Range
	for _, row := range resp.Values {
		strRow := make([]string, len(row))
		for i, cell := range row {
			strRow[i] = fmt.Sprintf("%v", cell)
		}
		out.Values = append(out.Values, strRow)
	}
	return out, nil
}

type UpdateValuesOut struct {
	UpdatedRange string `json:"updated_range"`
	UpdatedRows  int    `json:"updated_rows"`
	UpdatedCells int    `json:"updated_cells"`
}

func (c *GSheetsClient) UpdateValues(ctx context.Context, spreadsheetID, rangeA1 string, values [][]string) (UpdateValuesOut, error) {
	var out UpdateValuesOut

	path := fmt.Sprintf("/spreadsheets/%s/values/%s", url.PathEscape(spreadsheetID), url.PathEscape(rangeA1))
	query := url.Values{"valueInputOption": {"USER_ENTERED"}}

	payload := map[string]any{
		"range":  rangeA1,
		"values": values,
	}

	data, err := c.put(ctx, path, query, payload)
	if err != nil {
		return out, errors.Wrap(err, "update values")
	}

	var resp struct {
		UpdatedRange string `json:"updatedRange"`
		UpdatedRows  int    `json:"updatedRows"`
		UpdatedCells int    `json:"updatedCells"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode update response")
	}

	out.UpdatedRange = resp.UpdatedRange
	out.UpdatedRows = resp.UpdatedRows
	out.UpdatedCells = resp.UpdatedCells
	return out, nil
}

type AppendValuesOut struct {
	UpdatedRange string `json:"updated_range"`
	UpdatedRows  int    `json:"updated_rows"`
	UpdatedCells int    `json:"updated_cells"`
}

func (c *GSheetsClient) AppendValues(ctx context.Context, spreadsheetID, rangeA1 string, values [][]string) (AppendValuesOut, error) {
	var out AppendValuesOut

	path := fmt.Sprintf("/spreadsheets/%s/values/%s:append", url.PathEscape(spreadsheetID), url.PathEscape(rangeA1))
	query := url.Values{
		"valueInputOption": {"USER_ENTERED"},
		"insertDataOption": {"INSERT_ROWS"},
	}

	payload := map[string]any{
		"range":  rangeA1,
		"values": values,
	}

	data, err := c.post(ctx, path, query, payload)
	if err != nil {
		return out, errors.Wrap(err, "append values")
	}

	var resp struct {
		Updates struct {
			UpdatedRange string `json:"updatedRange"`
			UpdatedRows  int    `json:"updatedRows"`
			UpdatedCells int    `json:"updatedCells"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode append response")
	}

	out.UpdatedRange = resp.Updates.UpdatedRange
	out.UpdatedRows = resp.Updates.UpdatedRows
	out.UpdatedCells = resp.Updates.UpdatedCells
	return out, nil
}

type AddSheetOut struct {
	SheetID int    `json:"sheet_id"`
	Title   string `json:"title"`
}

func (c *GSheetsClient) AddSheet(ctx context.Context, spreadsheetID, title string) (AddSheetOut, error) {
	var out AddSheetOut

	payload := map[string]any{
		"requests": []map[string]any{
			{
				"addSheet": map[string]any{
					"properties": map[string]any{
						"title": title,
					},
				},
			},
		},
	}

	path := fmt.Sprintf("/spreadsheets/%s:batchUpdate", url.PathEscape(spreadsheetID))
	data, err := c.post(ctx, path, nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "add sheet")
	}

	var resp struct {
		Replies []struct {
			AddSheet struct {
				Properties struct {
					SheetID int    `json:"sheetId"`
					Title   string `json:"title"`
				} `json:"properties"`
			} `json:"addSheet"`
		} `json:"replies"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode add sheet response")
	}

	if len(resp.Replies) > 0 {
		out.SheetID = resp.Replies[0].AddSheet.Properties.SheetID
		out.Title = resp.Replies[0].AddSheet.Properties.Title
	}
	return out, nil
}

type RenameSheetOut struct {
	SpreadsheetID string `json:"spreadsheet_id"`
	Title         string `json:"title"`
}

func (c *GSheetsClient) RenameSheet(ctx context.Context, spreadsheetID string, sheetID int, title string) (RenameSheetOut, error) {
	var out RenameSheetOut

	payload := map[string]any{
		"requests": []map[string]any{
			{
				"updateSheetProperties": map[string]any{
					"properties": map[string]any{
						"sheetId": sheetID,
						"title":   title,
					},
					"fields": "title",
				},
			},
		},
	}

	path := fmt.Sprintf("/spreadsheets/%s:batchUpdate", url.PathEscape(spreadsheetID))
	if _, err := c.post(ctx, path, nil, payload); err != nil {
		return out, errors.Wrap(err, "rename sheet")
	}

	out.SpreadsheetID = spreadsheetID
	out.Title = title
	return out, nil
}

type CreateSpreadsheetOut struct {
	SpreadsheetID string `json:"spreadsheet_id"`
	Title         string `json:"title"`
	URL           string `json:"url"`
}

func (c *GSheetsClient) CreateSpreadsheet(ctx context.Context, title string) (CreateSpreadsheetOut, error) {
	var out CreateSpreadsheetOut

	payload := map[string]any{
		"properties": map[string]any{"title": title},
	}
	data, err := c.post(ctx, "/spreadsheets", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "create spreadsheet")
	}

	var resp struct {
		SpreadsheetID  string `json:"spreadsheetId"`
		SpreadsheetURL string `json:"spreadsheetUrl"`
		Properties     struct {
			Title string `json:"title"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode spreadsheet")
	}

	out.SpreadsheetID = resp.SpreadsheetID
	out.Title = resp.Properties.Title
	out.URL = resp.SpreadsheetURL
	return out, nil
}
