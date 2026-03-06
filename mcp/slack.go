package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

const slackAPIBase = "https://slack.com/api"

type SlackClient struct {
	Token string
}

func (c *SlackClient) do(ctx context.Context, method, endpoint string, params url.Values) (json.RawMessage, error) {
	var req *http.Request
	var err error

	apiURL := slackAPIBase + "/" + endpoint

	if method == http.MethodGet {
		if len(params) > 0 {
			apiURL += "?" + params.Encode()
		}
		req, err = http.NewRequestWithContext(ctx, method, apiURL, nil)
	} else {
		body := params.Encode()
		req, err = http.NewRequestWithContext(ctx, method, apiURL, strings.NewReader(body))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		slog.Error("slack api request failed", "method", method, "endpoint", endpoint, "latency", latency.Round(time.Millisecond).String(), "error", err)
		return nil, errors.Wrap(err, "slack api request")
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}

	var base struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}
	if !base.OK {
		slog.Warn("slack api error", "endpoint", endpoint, "error", base.Error, "latency", latency.Round(time.Millisecond).String())
		return nil, errors.Newf("slack api error: %s", base.Error)
	}

	slog.Info("slack api ok", "endpoint", endpoint, "latency", latency.Round(time.Millisecond).String())
	return json.RawMessage(data), nil
}

func (c *SlackClient) doJSON(ctx context.Context, endpoint string, payload any) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.Wrap(err, "marshal payload")
	}

	apiURL := slackAPIBase + "/" + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return nil, errors.Wrap(err, "create request")
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		slog.Error("slack api request failed", "endpoint", endpoint, "latency", latency.Round(time.Millisecond).String(), "error", err)
		return nil, errors.Wrap(err, "slack api request")
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read response")
	}

	var base struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respData, &base); err != nil {
		return nil, errors.Wrap(err, "decode response")
	}
	if !base.OK {
		slog.Warn("slack api error", "endpoint", endpoint, "error", base.Error, "latency", latency.Round(time.Millisecond).String())
		return nil, errors.Newf("slack api error: %s", base.Error)
	}

	slog.Info("slack api ok", "endpoint", endpoint, "latency", latency.Round(time.Millisecond).String())
	return json.RawMessage(respData), nil
}

// ReadChannel

type ReadChannelIn struct {
	ChannelID      string `json:"channel_id"`
	Cursor         string `json:"cursor,omitempty"`
	Latest         string `json:"latest,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Oldest         string `json:"oldest,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type ReadChannelOut struct {
	Messages   []json.RawMessage `json:"messages"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

func (c *SlackClient) ReadChannel(ctx context.Context, in ReadChannelIn) (ReadChannelOut, error) {
	var out ReadChannelOut
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	params := url.Values{
		"channel": {in.ChannelID},
		"limit":   {fmt.Sprintf("%d", limit)},
	}
	if in.Cursor != "" {
		params.Set("cursor", in.Cursor)
	}
	if in.Latest != "" {
		params.Set("latest", in.Latest)
	}
	if in.Oldest != "" {
		params.Set("oldest", in.Oldest)
	}
	data, err := c.do(ctx, http.MethodGet, "conversations.history", params)
	if err != nil {
		return out, errors.Wrap(err, "read channel")
	}
	var resp struct {
		Messages         []json.RawMessage `json:"messages"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode messages")
	}
	out.Messages = resp.Messages
	out.NextCursor = resp.ResponseMetadata.NextCursor
	return out, nil
}

// ReadThread

type ReadThreadIn struct {
	ChannelID      string `json:"channel_id"`
	Cursor         string `json:"cursor,omitempty"`
	Latest         string `json:"latest,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	MessageTS      string `json:"message_ts"`
	Oldest         string `json:"oldest,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type ReadThreadOut struct {
	Messages   []json.RawMessage `json:"messages"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

func (c *SlackClient) ReadThread(ctx context.Context, in ReadThreadIn) (ReadThreadOut, error) {
	var out ReadThreadOut
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	params := url.Values{
		"channel": {in.ChannelID},
		"limit":   {fmt.Sprintf("%d", limit)},
		"ts":      {in.MessageTS},
	}
	if in.Cursor != "" {
		params.Set("cursor", in.Cursor)
	}
	if in.Latest != "" {
		params.Set("latest", in.Latest)
	}
	if in.Oldest != "" {
		params.Set("oldest", in.Oldest)
	}
	data, err := c.do(ctx, http.MethodGet, "conversations.replies", params)
	if err != nil {
		return out, errors.Wrap(err, "read thread")
	}
	var resp struct {
		Messages         []json.RawMessage `json:"messages"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode thread")
	}
	out.Messages = resp.Messages
	out.NextCursor = resp.ResponseMetadata.NextCursor
	return out, nil
}

// ReadCanvas

type ReadCanvasOut struct {
	Content json.RawMessage `json:"content"`
}

func (c *SlackClient) ReadCanvas(ctx context.Context, canvasID string) (ReadCanvasOut, error) {
	var out ReadCanvasOut
	payload := map[string]any{
		"canvas_id": canvasID,
		"criteria":  map[string]any{"section_types": []string{"any_header"}},
	}
	data, err := c.doJSON(ctx, "canvases.sections.lookup", payload)
	if err != nil {
		return out, errors.Wrap(err, "read canvas")
	}
	out.Content = data
	return out, nil
}

// ReadUserProfile

type ReadUserProfileIn struct {
	IncludeLocale  bool   `json:"include_locale,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	UserID         string `json:"user_id,omitempty"`
}

type ReadUserProfileOut struct {
	User json.RawMessage `json:"user"`
}

func (c *SlackClient) ReadUserProfile(ctx context.Context, in ReadUserProfileIn) (ReadUserProfileOut, error) {
	var out ReadUserProfileOut
	userID := in.UserID
	if userID == "" {
		var err error
		userID, err = c.AuthTest(ctx)
		if err != nil {
			return out, errors.Wrap(err, "resolve current user")
		}
	}
	params := url.Values{
		"user": {userID},
	}
	if in.IncludeLocale {
		params.Set("include_locale", "true")
	}
	data, err := c.do(ctx, http.MethodGet, "users.info", params)
	if err != nil {
		return out, errors.Wrap(err, "read user profile")
	}
	var resp struct {
		User json.RawMessage `json:"user"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode user")
	}
	out.User = resp.User
	return out, nil
}

// SearchChannels

type SearchChannelsIn struct {
	ChannelTypes    string `json:"channel_types,omitempty"`
	Cursor          string `json:"cursor,omitempty"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	Query           string `json:"query"`
	ResponseFormat  string `json:"response_format,omitempty"`
}

type SearchChannelsOut struct {
	Channels   []json.RawMessage `json:"channels"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

func (c *SlackClient) SearchChannels(ctx context.Context, in SearchChannelsIn) (SearchChannelsOut, error) {
	var out SearchChannelsOut
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}
	types := in.ChannelTypes
	if types == "" {
		types = "public_channel"
	}
	params := url.Values{
		"limit": {fmt.Sprintf("%d", limit)},
		"query": {in.Query},
		"types": {types},
	}
	if in.Cursor != "" {
		params.Set("cursor", in.Cursor)
	}
	if !in.IncludeArchived {
		params.Set("exclude_archived", "true")
	}
	data, err := c.do(ctx, http.MethodGet, "conversations.list", params)
	if err != nil {
		return out, errors.Wrap(err, "search channels")
	}
	var resp struct {
		Channels         []json.RawMessage `json:"channels"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode channels")
	}
	out.Channels = resp.Channels
	out.NextCursor = resp.ResponseMetadata.NextCursor
	return out, nil
}

// SearchUsers

type SearchUsersIn struct {
	Cursor         string `json:"cursor,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Query          string `json:"query"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type SearchUsersOut struct {
	Members    []json.RawMessage `json:"members"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

func (c *SlackClient) SearchUsers(ctx context.Context, in SearchUsersIn) (SearchUsersOut, error) {
	var out SearchUsersOut
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}
	params := url.Values{
		"count": {fmt.Sprintf("%d", limit)},
	}
	if in.Cursor != "" {
		params.Set("cursor", in.Cursor)
	}
	data, err := c.do(ctx, http.MethodGet, "users.list", params)
	if err != nil {
		return out, errors.Wrap(err, "search users")
	}
	var resp struct {
		Members          []json.RawMessage `json:"members"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode users")
	}
	out.Members = resp.Members
	out.NextCursor = resp.ResponseMetadata.NextCursor
	return out, nil
}

// SearchPublic / SearchPublicAndPrivate

type SearchPublicIn struct {
	After            string `json:"after,omitempty"`
	Before           string `json:"before,omitempty"`
	ContentTypes     string `json:"content_types,omitempty"`
	ContextChannelID string `json:"context_channel_id,omitempty"`
	Cursor           string `json:"cursor,omitempty"`
	IncludeBots      bool   `json:"include_bots,omitempty"`
	IncludeContext   *bool  `json:"include_context,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	MaxContextLength int    `json:"max_context_length,omitempty"`
	Query            string `json:"query"`
	ResponseFormat   string `json:"response_format,omitempty"`
	Sort             string `json:"sort,omitempty"`
	SortDir          string `json:"sort_dir,omitempty"`
}

type SearchPublicAndPrivateIn struct {
	SearchPublicIn
	ChannelTypes string `json:"channel_types,omitempty"`
}

type SearchPublicOut struct {
	Messages json.RawMessage `json:"messages"`
}

func (c *SlackClient) buildSearchParams(in SearchPublicIn) url.Values {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}
	params := url.Values{
		"count": {fmt.Sprintf("%d", limit)},
		"query": {in.Query},
	}
	if in.After != "" {
		params.Set("after", in.After)
	}
	if in.Before != "" {
		params.Set("before", in.Before)
	}
	if in.ContentTypes != "" {
		params.Set("content_types", in.ContentTypes)
	}
	if in.ContextChannelID != "" {
		params.Set("context_channel_id", in.ContextChannelID)
	}
	if in.Cursor != "" {
		params.Set("cursor", in.Cursor)
	}
	if in.IncludeBots {
		params.Set("include_bots", "true")
	}
	if in.IncludeContext != nil && !*in.IncludeContext {
		params.Set("include_context", "false")
	}
	if in.MaxContextLength > 0 {
		params.Set("max_context_length", fmt.Sprintf("%d", in.MaxContextLength))
	}
	sort := in.Sort
	if sort == "" {
		sort = "score"
	}
	params.Set("sort", sort)
	sortDir := in.SortDir
	if sortDir == "" {
		sortDir = "desc"
	}
	params.Set("sort_dir", sortDir)
	return params
}

func (c *SlackClient) SearchPublic(ctx context.Context, in SearchPublicIn) (SearchPublicOut, error) {
	var out SearchPublicOut
	params := c.buildSearchParams(in)
	data, err := c.do(ctx, http.MethodGet, "search.messages", params)
	if err != nil {
		return out, errors.Wrap(err, "search public")
	}
	var resp struct {
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode search")
	}
	out.Messages = resp.Messages
	return out, nil
}

func (c *SlackClient) SearchPublicAndPrivate(ctx context.Context, in SearchPublicAndPrivateIn) (SearchPublicOut, error) {
	var out SearchPublicOut
	params := c.buildSearchParams(in.SearchPublicIn)
	if in.ChannelTypes != "" {
		params.Set("channel_types", in.ChannelTypes)
	}
	data, err := c.do(ctx, http.MethodGet, "search.messages", params)
	if err != nil {
		return out, errors.Wrap(err, "search public and private")
	}
	var resp struct {
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode search")
	}
	out.Messages = resp.Messages
	return out, nil
}

// SendMessage

type SlackSendMessageIn struct {
	ChannelID      string `json:"channel_id"`
	DraftID        string `json:"draft_id,omitempty"`
	Message        string `json:"message"`
	ReplyBroadcast bool   `json:"reply_broadcast,omitempty"`
	ThreadTS       string `json:"thread_ts,omitempty"`
}

type SlackSendMessageOut struct {
	Channel   string          `json:"channel"`
	Message   json.RawMessage `json:"message"`
	Timestamp string          `json:"ts"`
}

func (c *SlackClient) SendMessage(ctx context.Context, in SlackSendMessageIn) (SlackSendMessageOut, error) {
	var out SlackSendMessageOut
	params := url.Values{
		"channel": {in.ChannelID},
		"text":    {in.Message},
	}
	if in.ThreadTS != "" {
		params.Set("thread_ts", in.ThreadTS)
	}
	if in.ReplyBroadcast {
		params.Set("reply_broadcast", "true")
	}
	data, err := c.do(ctx, http.MethodPost, "chat.postMessage", params)
	if err != nil {
		return out, errors.Wrap(err, "send message")
	}
	var resp struct {
		Channel string          `json:"channel"`
		Message json.RawMessage `json:"message"`
		TS      string          `json:"ts"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode send response")
	}
	out.Channel = resp.Channel
	out.Message = resp.Message
	out.Timestamp = resp.TS

	if in.DraftID != "" {
		_ = c.deleteDraft(ctx, in.DraftID)
	}

	return out, nil
}

// SendMessageDraft

type SlackSendMessageDraftIn struct {
	ChannelID string `json:"channel_id"`
	Message   string `json:"message"`
	ThreadTS  string `json:"thread_ts,omitempty"`
}

type SlackSendMessageDraftOut struct {
	DraftID string `json:"draft_id"`
}

func (c *SlackClient) SendMessageDraft(ctx context.Context, in SlackSendMessageDraftIn) (SlackSendMessageDraftOut, error) {
	var out SlackSendMessageDraftOut

	userID, err := c.AuthTest(ctx)
	if err != nil {
		return out, err
	}
	channelID, err := c.OpenDM(ctx, userID)
	if err != nil {
		return out, err
	}

	var label string
	if in.ThreadTS != "" {
		label = fmt.Sprintf("[Draft for <#%s> thread %s]\n%s", in.ChannelID, in.ThreadTS, in.Message)
	} else {
		label = fmt.Sprintf("[Draft for <#%s>]\n%s", in.ChannelID, in.Message)
	}

	params := url.Values{
		"channel": {channelID},
		"text":    {label},
	}
	data, err := c.do(ctx, http.MethodPost, "chat.postMessage", params)
	if err != nil {
		return out, errors.Wrap(err, "send draft")
	}
	var resp struct {
		TS string `json:"ts"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode draft response")
	}
	out.DraftID = channelID + ":" + resp.TS
	return out, nil
}

func (c *SlackClient) deleteDraft(ctx context.Context, draftID string) error {
	parts := strings.SplitN(draftID, ":", 2)
	if len(parts) != 2 {
		return errors.New("invalid draft_id format")
	}
	params := url.Values{
		"channel": {parts[0]},
		"ts":      {parts[1]},
	}
	_, err := c.do(ctx, http.MethodPost, "chat.delete", params)
	return err
}

// ScheduleMessage

type SlackScheduleMessageIn struct {
	ChannelID      string `json:"channel_id"`
	Message        string `json:"message"`
	PostAt         int    `json:"post_at"`
	ReplyBroadcast bool   `json:"reply_broadcast,omitempty"`
	ThreadTS       string `json:"thread_ts,omitempty"`
}

type SlackScheduleMessageOut struct {
	Channel            string `json:"channel"`
	PostAt             int    `json:"post_at"`
	ScheduledMessageID string `json:"scheduled_message_id"`
}

func (c *SlackClient) ScheduleMessage(ctx context.Context, in SlackScheduleMessageIn) (SlackScheduleMessageOut, error) {
	var out SlackScheduleMessageOut
	params := url.Values{
		"channel": {in.ChannelID},
		"post_at": {fmt.Sprintf("%d", in.PostAt)},
		"text":    {in.Message},
	}
	if in.ThreadTS != "" {
		params.Set("thread_ts", in.ThreadTS)
	}
	if in.ReplyBroadcast {
		params.Set("reply_broadcast", "true")
	}
	data, err := c.do(ctx, http.MethodPost, "chat.scheduleMessage", params)
	if err != nil {
		return out, errors.Wrap(err, "schedule message")
	}
	var resp struct {
		Channel            string `json:"channel"`
		PostAt             int    `json:"post_at"`
		ScheduledMessageID string `json:"scheduled_message_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode schedule response")
	}
	out.Channel = resp.Channel
	out.PostAt = resp.PostAt
	out.ScheduledMessageID = resp.ScheduledMessageID
	return out, nil
}

// CreateCanvas

type CreateCanvasIn struct {
	Content string `json:"content"`
	Title   string `json:"title"`
}

type CreateCanvasOut struct {
	CanvasID string `json:"canvas_id"`
}

func (c *SlackClient) CreateCanvas(ctx context.Context, in CreateCanvasIn) (CreateCanvasOut, error) {
	var out CreateCanvasOut
	payload := map[string]any{
		"title":         in.Title,
		"document_content": map[string]string{
			"type":     "markdown",
			"markdown": in.Content,
		},
	}
	data, err := c.doJSON(ctx, "canvases.create", payload)
	if err != nil {
		return out, errors.Wrap(err, "create canvas")
	}
	var resp struct {
		CanvasID string `json:"canvas_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode canvas response")
	}
	out.CanvasID = resp.CanvasID
	return out, nil
}

// Auth helpers

func (c *SlackClient) AuthTest(ctx context.Context) (string, error) {
	data, err := c.do(ctx, http.MethodPost, "auth.test", nil)
	if err != nil {
		return "", errors.Wrap(err, "auth test")
	}
	var resp struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", errors.Wrap(err, "decode auth test")
	}
	return resp.UserID, nil
}

func (c *SlackClient) OpenDM(ctx context.Context, userID string) (string, error) {
	params := url.Values{
		"users": {userID},
	}
	data, err := c.do(ctx, http.MethodPost, "conversations.open", params)
	if err != nil {
		return "", errors.Wrap(err, "open dm")
	}
	var resp struct {
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", errors.Wrap(err, "decode open dm")
	}
	return resp.Channel.ID, nil
}
