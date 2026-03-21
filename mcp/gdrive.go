package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/cockroachdb/errors"
)

const gdriveAPIBase = "https://www.googleapis.com/drive/v3"
const gdriveUploadBase = "https://www.googleapis.com/upload/drive/v3"

type GDriveClient struct {
	Token string
}

type GDriveFile struct {
	ID           string   `json:"id"`
	MimeType     string   `json:"mimeType"`
	ModifiedTime string   `json:"modifiedTime"`
	Name         string   `json:"name"`
	Parents      []string `json:"parents"`
	Size         string   `json:"size"`
	WebViewLink  string   `json:"webViewLink"`
}

type GDrivePermission struct {
	Email string `json:"emailAddress"`
	ID    string `json:"id"`
	Role  string `json:"role"`
	Type  string `json:"type"`
}

func (c *GDriveClient) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (json.RawMessage, error) {
	apiURL := gdriveAPIBase + path
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
		return nil, errors.Wrap(err, "drive api request")
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
			return nil, errors.Newf("drive api error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, errors.Newf("drive api error (%d): %s", resp.StatusCode, string(data))
	}

	return json.RawMessage(data), nil
}

func (c *GDriveClient) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, "")
}

func (c *GDriveClient) post(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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

func (c *GDriveClient) getRaw(ctx context.Context, fullURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "drive api request")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}

	if resp.StatusCode >= 400 {
		return nil, errors.Newf("drive api error (%d): %s", resp.StatusCode, string(data))
	}

	return data, nil
}

type ListFilesOut struct {
	Files         []GDriveFile `json:"files"`
	NextPageToken string       `json:"next_page_token,omitempty"`
}

const maxListFiles = 25

func (c *GDriveClient) ListFiles(ctx context.Context, query string, pageSize int, pageToken string, orderBy string) (ListFilesOut, error) {
	var out ListFilesOut
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > maxListFiles {
		pageSize = maxListFiles
	}
	if orderBy == "" {
		orderBy = "modifiedTime desc"
	}

	params := url.Values{
		"fields":   {"files(id,mimeType,modifiedTime,name,parents,size,webViewLink),nextPageToken"},
		"orderBy":  {orderBy},
		"pageSize": {fmt.Sprintf("%d", pageSize)},
	}
	if query != "" {
		params.Set("q", query)
	}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}

	data, err := c.get(ctx, "/files", params)
	if err != nil {
		return out, errors.Wrap(err, "list files")
	}

	var resp struct {
		Files         []GDriveFile `json:"files"`
		NextPageToken string       `json:"nextPageToken"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode list files")
	}

	out.Files = resp.Files
	out.NextPageToken = resp.NextPageToken
	return out, nil
}

type GetFileOut struct {
	File GDriveFile `json:"file"`
}

func (c *GDriveClient) GetFile(ctx context.Context, fileID string) (GetFileOut, error) {
	var out GetFileOut
	params := url.Values{
		"fields": {"*"},
	}

	data, err := c.get(ctx, "/files/"+url.PathEscape(fileID), params)
	if err != nil {
		return out, errors.Wrap(err, "get file")
	}

	if err := json.Unmarshal(data, &out.File); err != nil {
		return out, errors.Wrap(err, "decode file")
	}

	return out, nil
}

type GetFileContentOut struct {
	Content  string `json:"content"`
	MimeType string `json:"mime_type"`
	Name     string `json:"name"`
}

func (c *GDriveClient) GetFileContent(ctx context.Context, fileID string) (GetFileContentOut, error) {
	var out GetFileContentOut

	fileMeta, err := c.GetFile(ctx, fileID)
	if err != nil {
		return out, errors.Wrap(err, "get file metadata")
	}
	out.Name = fileMeta.File.Name
	out.MimeType = fileMeta.File.MimeType

	exportMimeTypes := map[string]string{
		"application/vnd.google-apps.document":     "text/plain",
		"application/vnd.google-apps.spreadsheet":   "text/csv",
		"application/vnd.google-apps.presentation":  "text/plain",
	}

	escapedID := url.PathEscape(fileID)
	var rawURL string
	if exportMime, ok := exportMimeTypes[fileMeta.File.MimeType]; ok {
		params := url.Values{"mimeType": {exportMime}}
		rawURL = gdriveAPIBase + "/files/" + escapedID + "/export?" + params.Encode()
	} else {
		params := url.Values{"alt": {"media"}}
		rawURL = gdriveAPIBase + "/files/" + escapedID + "?" + params.Encode()
	}

	rawData, err := c.getRaw(ctx, rawURL)
	if err != nil {
		return out, errors.Wrap(err, "get file content")
	}

	out.Content = string(rawData)
	return out, nil
}

func (c *GDriveClient) SearchFiles(ctx context.Context, query string, pageSize int, pageToken string) (ListFilesOut, error) {
	return c.ListFiles(ctx, query, pageSize, pageToken, "")
}

type CreateFileOut struct {
	File GDriveFile `json:"file"`
}

func (c *GDriveClient) CreateFile(ctx context.Context, name, mimeType, content, parentID string) (CreateFileOut, error) {
	var out CreateFileOut

	metadata := map[string]any{
		"name":     name,
		"mimeType": mimeType,
	}
	if parentID != "" {
		metadata["parents"] = []string{parentID}
	}

	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return out, errors.Wrap(err, "marshal metadata")
	}

	fields := "id,mimeType,modifiedTime,name,parents,size,webViewLink"

	if content == "" && strings.HasPrefix(mimeType, "application/vnd.google-apps.") {
		apiURL := gdriveAPIBase + "/files?fields=" + fields
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(metaJSON))
		if err != nil {
			return out, errors.Wrap(err, "create request")
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return out, errors.Wrap(err, "drive api request")
		}
		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return out, errors.Wrap(err, "read response")
		}
		if resp.StatusCode >= 300 {
			return out, errors.Newf("drive api error (%d): %s", resp.StatusCode, string(data))
		}
		if err := json.Unmarshal(data, &out.File); err != nil {
			return out, errors.Wrap(err, "decode file")
		}
		return out, nil
	}

	contentMime := mimeType
	if strings.HasPrefix(mimeType, "application/vnd.google-apps.") {
		contentMime = "text/plain"
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	metaPart, err := writer.CreatePart(map[string][]string{
		"Content-Type": {"application/json"},
	})
	if err != nil {
		return out, errors.Wrap(err, "create metadata part")
	}
	if _, err := metaPart.Write(metaJSON); err != nil {
		return out, errors.Wrap(err, "write metadata part")
	}

	contentPart, err := writer.CreatePart(map[string][]string{
		"Content-Type": {contentMime},
	})
	if err != nil {
		return out, errors.Wrap(err, "create content part")
	}
	if _, err := contentPart.Write([]byte(content)); err != nil {
		return out, errors.Wrap(err, "write content part")
	}

	if err := writer.Close(); err != nil {
		return out, errors.Wrap(err, "close multipart writer")
	}

	uploadURL := gdriveUploadBase + "/files?uploadType=multipart&fields=" + fields

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &buf)
	if err != nil {
		return out, errors.Wrap(err, "create request")
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, errors.Wrap(err, "drive api request")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, errors.Wrap(err, "read response")
	}

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error.Message != "" {
			return out, errors.Newf("drive api error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return out, errors.Newf("drive api error (%d): %s", resp.StatusCode, string(data))
	}

	if err := json.Unmarshal(data, &out.File); err != nil {
		return out, errors.Wrap(err, "decode created file")
	}

	return out, nil
}

type TrashFileOut struct {
	ID string `json:"id"`
}

func (c *GDriveClient) TrashFile(ctx context.Context, fileID string) (TrashFileOut, error) {
	var out TrashFileOut
	payload := map[string]any{"trashed": true}
	data, err := json.Marshal(payload)
	if err != nil {
		return out, errors.Wrap(err, "marshal payload")
	}
	raw, err := c.do(ctx, http.MethodPatch, "/files/"+url.PathEscape(fileID), nil, bytes.NewReader(data), "application/json")
	if err != nil {
		return out, errors.Wrap(err, "trash file")
	}
	var file GDriveFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return out, errors.Wrap(err, "decode trashed file")
	}
	out.ID = file.ID
	return out, nil
}

type ListPermissionsOut struct {
	Permissions []GDrivePermission `json:"permissions"`
}

func (c *GDriveClient) ListPermissions(ctx context.Context, fileID string) (ListPermissionsOut, error) {
	var out ListPermissionsOut
	params := url.Values{
		"fields": {"permissions(emailAddress,id,role,type)"},
	}

	data, err := c.get(ctx, "/files/"+url.PathEscape(fileID)+"/permissions", params)
	if err != nil {
		return out, errors.Wrap(err, "list permissions")
	}

	var resp struct {
		Permissions []GDrivePermission `json:"permissions"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode permissions")
	}

	out.Permissions = resp.Permissions
	return out, nil
}

type AddPermissionOut struct {
	Permission GDrivePermission `json:"permission"`
}

func (c *GDriveClient) AddPermission(ctx context.Context, fileID, email, role string) (AddPermissionOut, error) {
	var out AddPermissionOut

	payload := map[string]any{
		"emailAddress": email,
		"role":         role,
		"type":         "user",
	}

	data, err := c.post(ctx, "/files/"+url.PathEscape(fileID)+"/permissions", nil, payload)
	if err != nil {
		return out, errors.Wrap(err, "add permission")
	}

	if err := json.Unmarshal(data, &out.Permission); err != nil {
		return out, errors.Wrap(err, "decode permission")
	}

	return out, nil
}
