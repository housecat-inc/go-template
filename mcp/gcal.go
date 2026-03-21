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

var gcalAPIBase = "https://www.googleapis.com/calendar/v3"

type GCalClient struct {
	BaseURL string
	Token   string
}

func (c *GCalClient) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return gcalAPIBase
}

type GCalAttendee struct {
	Email          string `json:"email"`
	ResponseStatus string `json:"responseStatus,omitempty"`
}

type GCalCalendar struct {
	AccessRole  string `json:"accessRole,omitempty"`
	Description string `json:"description,omitempty"`
	ID          string `json:"id"`
	Primary     bool   `json:"primary,omitempty"`
	Summary     string `json:"summary,omitempty"`
	TimeZone    string `json:"timeZone,omitempty"`
}

type GCalDateTime struct {
	Date     string `json:"date,omitempty"`
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type GCalExtendedProperties struct {
	Private map[string]string `json:"private,omitempty"`
}

type GCalEvent struct {
	Attendees          []GCalAttendee          `json:"attendees,omitempty"`
	Description        string                  `json:"description,omitempty"`
	End                *GCalDateTime           `json:"end,omitempty"`
	ExtendedProperties *GCalExtendedProperties `json:"extendedProperties,omitempty"`
	HtmlLink           string                  `json:"htmlLink,omitempty"`
	ID                 string                  `json:"id,omitempty"`
	Location           string                  `json:"location,omitempty"`
	Start              *GCalDateTime           `json:"start,omitempty"`
	Status             string                  `json:"status,omitempty"`
	Summary            string                  `json:"summary,omitempty"`
}

func (c *GCalClient) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (json.RawMessage, error) {
	apiURL := c.baseURL() + path
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
		return nil, errors.Wrap(err, "gcal api request")
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
			return nil, errors.Newf("gcal api error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, errors.Newf("gcal api error (%d): %s", resp.StatusCode, string(data))
	}

	return json.RawMessage(data), nil
}

func (c *GCalClient) get(ctx context.Context, path string, query url.Values) (json.RawMessage, error) {
	return c.do(ctx, http.MethodGet, path, query, nil, "")
}

func (c *GCalClient) post(ctx context.Context, path string, query url.Values, payload any) (json.RawMessage, error) {
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

type ListCalendarsOut struct {
	Calendars []GCalCalendar `json:"calendars"`
}

func (c *GCalClient) ListCalendars(ctx context.Context) (ListCalendarsOut, error) {
	var out ListCalendarsOut
	data, err := c.get(ctx, "/users/me/calendarList", nil)
	if err != nil {
		return out, errors.Wrap(err, "list calendars")
	}

	var resp struct {
		Items []GCalCalendar `json:"items"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode calendars")
	}
	out.Calendars = resp.Items
	return out, nil
}

type ListEventsOut struct {
	Events        []GCalEvent `json:"events"`
	NextPageToken string      `json:"next_page_token,omitempty"`
}

const maxListEvents = 50

func (c *GCalClient) ListEvents(ctx context.Context, calendarID, timeMin, timeMax string, maxResults int, pageToken, query string) (ListEventsOut, error) {
	var out ListEventsOut
	if calendarID == "" {
		calendarID = "primary"
	}
	if maxResults <= 0 {
		maxResults = 25
	}
	if maxResults > maxListEvents {
		maxResults = maxListEvents
	}

	params := url.Values{
		"maxResults":   {fmt.Sprintf("%d", maxResults)},
		"singleEvents": {"true"},
		"orderBy":      {"startTime"},
	}
	if timeMin != "" {
		params.Set("timeMin", timeMin)
	}
	if timeMax != "" {
		params.Set("timeMax", timeMax)
	}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	if query != "" {
		params.Set("q", query)
	}

	data, err := c.get(ctx, "/calendars/"+url.PathEscape(calendarID)+"/events", params)
	if err != nil {
		return out, errors.Wrap(err, "list events")
	}

	var resp struct {
		Items         []GCalEvent `json:"items"`
		NextPageToken string      `json:"nextPageToken"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return out, errors.Wrap(err, "decode events")
	}
	out.Events = resp.Items
	out.NextPageToken = resp.NextPageToken
	return out, nil
}

type GetEventOut = GCalEvent

func (c *GCalClient) GetEvent(ctx context.Context, calendarID, eventID string) (GetEventOut, error) {
	var out GetEventOut
	if calendarID == "" {
		calendarID = "primary"
	}

	data, err := c.get(ctx, "/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID), nil)
	if err != nil {
		return out, errors.Wrap(err, "get event")
	}

	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode event")
	}
	return out, nil
}

type CreateEventOut = GCalEvent

func (c *GCalClient) PrimaryTimezone(ctx context.Context) string {
	data, err := c.get(ctx, "/users/me/settings/timezone", nil)
	if err != nil {
		return ""
	}
	var setting struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &setting); err != nil {
		return ""
	}
	return setting.Value
}

func (c *GCalClient) CreateEvent(ctx context.Context, calendarID, summary, description, start, end, timezone string, attendees []string, location string, draft bool) (CreateEventOut, error) {
	var out CreateEventOut
	if calendarID == "" {
		calendarID = "primary"
	}

	startObj := map[string]string{"dateTime": start}
	endObj := map[string]string{"dateTime": end}
	if timezone != "" {
		startObj["timeZone"] = timezone
		endObj["timeZone"] = timezone
	}

	event := map[string]any{
		"summary": summary,
		"start":   startObj,
		"end":     endObj,
	}
	if description != "" {
		event["description"] = description
	}
	if location != "" {
		event["location"] = location
	}
	if len(attendees) > 0 {
		attendeeList := make([]map[string]string, len(attendees))
		for i, email := range attendees {
			attendeeList[i] = map[string]string{"email": email}
		}
		event["attendees"] = attendeeList
	}
	if draft {
		event["extendedProperties"] = map[string]any{
			"private": map[string]string{"housecat_draft": "true"},
		}
	}

	data, err := c.post(ctx, "/calendars/"+url.PathEscape(calendarID)+"/events", nil, event)
	if err != nil {
		return out, errors.Wrap(err, "create event")
	}

	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode created event")
	}
	return out, nil
}

type UpdateEventIn struct {
	Attendees          []string           `json:"-"`
	CalendarID         string             `json:"-"`
	Description        *string            `json:"-"`
	End                string             `json:"-"`
	EventID            string             `json:"-"`
	ExtendedProperties map[string]string  `json:"-"`
	Location           *string            `json:"-"`
	SendUpdates        string             `json:"-"`
	Start              string             `json:"-"`
	Summary            string             `json:"-"`
	Timezone           string             `json:"-"`
}

type UpdateEventOut = GCalEvent

func (c *GCalClient) UpdateEvent(ctx context.Context, in UpdateEventIn) (UpdateEventOut, error) {
	var out UpdateEventOut
	calendarID := in.CalendarID
	if calendarID == "" {
		calendarID = "primary"
	}

	event := map[string]any{}
	if in.Summary != "" {
		event["summary"] = in.Summary
	}
	if in.Description != nil {
		event["description"] = *in.Description
	}
	if in.Start != "" {
		s := map[string]string{"dateTime": in.Start}
		if in.Timezone != "" {
			s["timeZone"] = in.Timezone
		}
		event["start"] = s
	}
	if in.End != "" {
		e := map[string]string{"dateTime": in.End}
		if in.Timezone != "" {
			e["timeZone"] = in.Timezone
		}
		event["end"] = e
	}
	if in.Location != nil {
		event["location"] = *in.Location
	}
	if in.Attendees != nil {
		attendeeList := make([]map[string]string, len(in.Attendees))
		for i, email := range in.Attendees {
			attendeeList[i] = map[string]string{"email": email}
		}
		event["attendees"] = attendeeList
	}
	if len(in.ExtendedProperties) > 0 {
		event["extendedProperties"] = map[string]any{
			"private": in.ExtendedProperties,
		}
	}

	query := url.Values{}
	if in.SendUpdates != "" {
		query.Set("sendUpdates", in.SendUpdates)
	}

	body, err := json.Marshal(event)
	if err != nil {
		return out, errors.Wrap(err, "marshal event")
	}

	data, err := c.do(ctx, http.MethodPatch, "/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(in.EventID), query, bytes.NewReader(body), "application/json")
	if err != nil {
		return out, errors.Wrap(err, "update event")
	}

	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode updated event")
	}
	return out, nil
}

type DeleteEventOut struct {
	EventID string `json:"event_id"`
}

func (c *GCalClient) DeleteEvent(ctx context.Context, calendarID, eventID string) (DeleteEventOut, error) {
	var out DeleteEventOut
	if calendarID == "" {
		calendarID = "primary"
	}
	_, err := c.do(ctx, http.MethodDelete, "/calendars/"+url.PathEscape(calendarID)+"/events/"+url.PathEscape(eventID), nil, nil, "")
	if err != nil {
		return out, errors.Wrap(err, "delete event")
	}
	out.EventID = eventID
	return out, nil
}

type QuickAddOut = GCalEvent

func (c *GCalClient) QuickAdd(ctx context.Context, calendarID, text string) (QuickAddOut, error) {
	var out QuickAddOut
	if calendarID == "" {
		calendarID = "primary"
	}

	params := url.Values{
		"text": {text},
	}

	data, err := c.post(ctx, "/calendars/"+url.PathEscape(calendarID)+"/events/quickAdd", params, nil)
	if err != nil {
		return out, errors.Wrap(err, "quick add event")
	}

	if err := json.Unmarshal(data, &out); err != nil {
		return out, errors.Wrap(err, "decode quick add event")
	}
	return out, nil
}
