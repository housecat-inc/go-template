package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cockroachdb/errors"
)

const slackAPIBase = "https://slack.com/api"

type SlackClient struct {
	Token string
}

type SlackChannel struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	IsMember       bool   `json:"is_member"`
	NumMembers     int    `json:"num_members"`
	Purpose        slackTopic `json:"purpose"`
	Topic          slackTopic `json:"topic"`
}

type slackTopic struct {
	Value string `json:"value"`
}

type SlackMessage struct {
	Text    string `json:"text"`
	TS      string `json:"ts"`
	Type    string `json:"type"`
	User    string `json:"user"`
}

type SlackUser struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	RealName string           `json:"real_name"`
	Profile  SlackUserProfile `json:"profile"`
}

type SlackUserProfile struct {
	DisplayName string `json:"display_name"`
	RealName    string `json:"real_name"`
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
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
		return nil, errors.Newf("slack api error: %s", base.Error)
	}

	return json.RawMessage(data), nil
}

type ListChannelsOut struct {
	Channels   []SlackChannel `json:"channels"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

func (c *SlackClient) ListChannels(ctx context.Context, cursor string, limit int) (ListChannelsOut, error) {
	var out ListChannelsOut
	if limit <= 0 {
		limit = 100
	}
	params := url.Values{
		"exclude_archived": {"true"},
		"limit":            {fmt.Sprintf("%d", limit)},
		"types":            {"public_channel,private_channel"},
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}
	data, err := c.do(ctx, http.MethodGet, "conversations.list", params)
	if err != nil {
		return out, errors.Wrap(err, "list channels")
	}
	var resp struct {
		Channels         []SlackChannel `json:"channels"`
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

type GetChannelHistoryOut struct {
	Messages []SlackMessage `json:"messages"`
}

func (c *SlackClient) GetChannelHistory(ctx context.Context, channelID string, limit int) (GetChannelHistoryOut, error) {
	var out GetChannelHistoryOut
	if limit <= 0 {
		limit = 20
	}
	params := url.Values{
		"channel": {channelID},
		"limit":   {fmt.Sprintf("%d", limit)},
	}
	data, err := c.do(ctx, http.MethodGet, "conversations.history", params)
	if err != nil {
		return out, errors.Wrap(err, "get channel history")
	}
	var resp struct {
		Messages []SlackMessage `json:"messages"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode messages")
	}
	out.Messages = resp.Messages
	return out, nil
}

type PostMessageOut struct {
	Channel   string       `json:"channel"`
	Message   SlackMessage `json:"message"`
	Timestamp string       `json:"ts"`
}

func (c *SlackClient) PostMessage(ctx context.Context, channelID, text string) (PostMessageOut, error) {
	var out PostMessageOut
	params := url.Values{
		"channel": {channelID},
		"text":    {text},
	}
	data, err := c.do(ctx, http.MethodPost, "chat.postMessage", params)
	if err != nil {
		return out, errors.Wrap(err, "post message")
	}
	var resp struct {
		Channel string       `json:"channel"`
		Message SlackMessage `json:"message"`
		TS      string       `json:"ts"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode post response")
	}
	out.Channel = resp.Channel
	out.Message = resp.Message
	out.Timestamp = resp.TS
	return out, nil
}

type SearchMessagesOut struct {
	Matches []SlackSearchMatch `json:"matches"`
	Total   int                `json:"total"`
}

type SlackSearchMatch struct {
	Channel   SlackSearchChannel `json:"channel"`
	Text      string             `json:"text"`
	Timestamp string             `json:"ts"`
	User      string             `json:"username"`
}

type SlackSearchChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *SlackClient) SearchMessages(ctx context.Context, query string, count int) (SearchMessagesOut, error) {
	var out SearchMessagesOut
	if count <= 0 {
		count = 20
	}
	params := url.Values{
		"count": {fmt.Sprintf("%d", count)},
		"query": {query},
	}
	data, err := c.do(ctx, http.MethodGet, "search.messages", params)
	if err != nil {
		return out, errors.Wrap(err, "search messages")
	}
	var resp struct {
		Messages struct {
			Matches []SlackSearchMatch `json:"matches"`
			Total   int                `json:"total"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode search response")
	}
	out.Matches = resp.Messages.Matches
	out.Total = resp.Messages.Total
	return out, nil
}

type GetUserProfileOut struct {
	User SlackUser `json:"user"`
}

func (c *SlackClient) GetUserProfile(ctx context.Context, userID string) (GetUserProfileOut, error) {
	var out GetUserProfileOut
	params := url.Values{
		"user": {userID},
	}
	data, err := c.do(ctx, http.MethodGet, "users.info", params)
	if err != nil {
		return out, errors.Wrap(err, "get user profile")
	}
	var resp struct {
		User SlackUser `json:"user"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode user")
	}
	out.User = resp.User
	return out, nil
}

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

type DraftMessageOut struct {
	ChannelID string       `json:"channel_id"`
	Message   SlackMessage `json:"message"`
}

func (c *SlackClient) DraftMessage(ctx context.Context, text string) (DraftMessageOut, error) {
	var out DraftMessageOut
	userID, err := c.AuthTest(ctx)
	if err != nil {
		return out, err
	}
	channelID, err := c.OpenDM(ctx, userID)
	if err != nil {
		return out, err
	}
	post, err := c.PostMessage(ctx, channelID, text)
	if err != nil {
		return out, err
	}
	out.ChannelID = channelID
	out.Message = post.Message
	return out, nil
}
