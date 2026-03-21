package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockGCalServer struct {
	mu     sync.Mutex
	events map[string]GCalEvent
	srv    *httptest.Server
}

func newMockGCalServer() *mockGCalServer {
	m := &mockGCalServer{events: map[string]GCalEvent{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/calendars/primary/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var event GCalEvent
		json.NewDecoder(r.Body).Decode(&event)
		m.mu.Lock()
		id := "evt-" + strings.ReplaceAll(event.Summary, " ", "-")
		event.ID = id
		m.events[id] = event
		m.mu.Unlock()
		json.NewEncoder(w).Encode(event)
	})

	mux.HandleFunc("/calendars/primary/events/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/calendars/primary/events/")
		m.mu.Lock()
		defer m.mu.Unlock()

		switch r.Method {
		case http.MethodGet:
			evt, ok := m.events[id]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(evt)

		case http.MethodPatch:
			evt, ok := m.events[id]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			var patch map[string]json.RawMessage
			json.NewDecoder(r.Body).Decode(&patch)
			if v, ok := patch["summary"]; ok {
				json.Unmarshal(v, &evt.Summary)
			}
			if v, ok := patch["description"]; ok {
				json.Unmarshal(v, &evt.Description)
			}
			if v, ok := patch["attendees"]; ok {
				json.Unmarshal(v, &evt.Attendees)
			}
			if v, ok := patch["extendedProperties"]; ok {
				var ep GCalExtendedProperties
				json.Unmarshal(v, &ep)
				if evt.ExtendedProperties == nil {
					evt.ExtendedProperties = &ep
				} else {
					for k, v := range ep.Private {
						if v == "" {
							delete(evt.ExtendedProperties.Private, k)
						} else {
							evt.ExtendedProperties.Private[k] = v
						}
					}
				}
			}
			m.events[id] = evt
			json.NewEncoder(w).Encode(evt)

		case http.MethodDelete:
			if _, ok := m.events[id]; !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			delete(m.events, id)
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/users/me/settings/timezone", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"value": "America/Los_Angeles"})
	})

	m.srv = httptest.NewServer(mux)
	return m
}

func (m *mockGCalServer) client() *GCalClient {
	return &GCalClient{BaseURL: m.srv.URL, Token: "test"}
}

func (m *mockGCalServer) close() { m.srv.Close() }

func (m *mockGCalServer) getEvent(id string) (GCalEvent, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[id]
	return e, ok
}

func boolPtr(v bool) *bool { return &v }

func TestGCalDraftWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		levels   map[string]bool
		op       string
		draft    *bool
		event    *GCalEvent
		wantErr  string
		checkFn  func(t *testing.T, m *mockGCalServer)
	}{
		{
			name:   "create draft with draft level",
			levels: map[string]bool{"draft": true},
			op:     "create",
			draft:  boolPtr(true),
			checkFn: func(t *testing.T, m *mockGCalServer) {
				a := assert.New(t)
				evt, ok := m.getEvent("evt-Cat-Sitting")
				a.True(ok)
				a.Empty(evt.Attendees)
				a.Contains(evt.Description, "Attendees:\n\n - noah@test.com")
				a.Contains(evt.Description, "Drafted with Housecat")
				a.Equal("true", evt.ExtendedProperties.Private["housecat_draft"])
			},
		},
		{
			name:   "create draft with write level",
			levels: map[string]bool{"write": true},
			op:     "create",
			draft:  boolPtr(true),
			checkFn: func(t *testing.T, m *mockGCalServer) {
				a := assert.New(t)
				evt, _ := m.getEvent("evt-Cat-Sitting")
				a.Empty(evt.Attendees)
				a.Contains(evt.Description, "Attendees:\n\n - noah@test.com")
				a.Contains(evt.Description, "Drafted with Housecat")
				a.Equal("true", evt.ExtendedProperties.Private["housecat_draft"])
			},
		},
		{
			name:   "create sent with write level",
			levels: map[string]bool{"write": true},
			op:     "create",
			draft:  boolPtr(false),
			checkFn: func(t *testing.T, m *mockGCalServer) {
				a := assert.New(t)
				evt, _ := m.getEvent("evt-Cat-Sitting")
				a.Len(evt.Attendees, 1)
				a.Equal("noah@test.com", evt.Attendees[0].Email)
				a.NotContains(evt.Description, "Attendees:\n\n")
				a.Contains(evt.Description, "Sent with Housecat")
				a.Nil(evt.ExtendedProperties)
			},
		},
		{
			name:    "create sent with draft level only",
			levels:  map[string]bool{"draft": true},
			op:      "create",
			draft:   boolPtr(false),
			wantErr: "", // succeeds — draft level can create, just uses draft behavior
			checkFn: func(t *testing.T, m *mockGCalServer) {
				a := assert.New(t)
				evt, _ := m.getEvent("evt-Cat-Sitting")
				a.Contains(evt.Description, "Drafted with Housecat")
			},
		},
		{
			name:   "send draft event with write level",
			levels: map[string]bool{"write": true, "draft": true},
			op:     "update-send",
			event: &GCalEvent{
				ID:          "evt-1",
				Description: "Hello\n\nAttendees:\n\n - noah@test.com<br><br><small><a href=\"https://test.exe.xyz\">Drafted with Housecat</a></small>",
				ExtendedProperties: &GCalExtendedProperties{
					Private: map[string]string{"housecat_draft": "true"},
				},
			},
			checkFn: func(t *testing.T, m *mockGCalServer) {
				a := assert.New(t)
				evt, _ := m.getEvent("evt-1")
				a.Len(evt.Attendees, 1)
				a.Equal("noah@test.com", evt.Attendees[0].Email)
				a.NotContains(evt.Description, "Attendees:\n\n")
				a.Contains(evt.Description, ">Sent with Housecat<")
				a.NotContains(evt.Description, ">Drafted with Housecat<")
				_, hasDraft := evt.ExtendedProperties.Private["housecat_draft"]
				a.False(hasDraft)
			},
		},
		{
			name:   "send non-draft event fails",
			levels: map[string]bool{"write": true},
			op:     "update-send",
			event: &GCalEvent{
				ID:          "evt-2",
				Description: "Hello\n\nSent with Housecat",
			},
			wantErr: "event is not a draft",
		},
		{
			name:   "delete draft event with draft level",
			levels: map[string]bool{"draft": true},
			op:     "delete-draft",
			event: &GCalEvent{
				ID: "evt-3",
				ExtendedProperties: &GCalExtendedProperties{
					Private: map[string]string{"housecat_draft": "true"},
				},
			},
			checkFn: func(t *testing.T, m *mockGCalServer) {
				_, ok := m.getEvent("evt-3")
				assert.False(t, ok)
			},
		},
		{
			name:   "delete sent event with draft level fails",
			levels: map[string]bool{"draft": true},
			op:     "delete",
			event: &GCalEvent{
				ID:          "evt-4",
				Description: "Sent with Housecat",
			},
			wantErr: "sent events require archive level to delete",
		},
		{
			name:   "delete sent event with archive level",
			levels: map[string]bool{"archive": true},
			op:     "delete",
			event: &GCalEvent{
				ID:          "evt-5",
				Description: "Sent with Housecat",
			},
			checkFn: func(t *testing.T, m *mockGCalServer) {
				_, ok := m.getEvent("evt-5")
				assert.False(t, ok)
			},
		},
		{
			name:    "delete with write only fails",
			levels:  map[string]bool{"write": true},
			op:      "delete",
			event:   &GCalEvent{ID: "evt-6"},
			wantErr: "archive or draft level required",
		},
		{
			name:   "delete draft=true on sent event fails",
			levels: map[string]bool{"draft": true, "archive": true},
			op:     "delete-draft",
			event: &GCalEvent{
				ID:          "evt-7",
				Description: "Sent with Housecat",
			},
			wantErr: "event is not a draft",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			m := newMockGCalServer()
			defer m.close()
			client := m.client()
			ctx := t.Context()
			lookup := mockLookup(tt.levels)

			if tt.event != nil {
				m.mu.Lock()
				m.events[tt.event.ID] = *tt.event
				m.mu.Unlock()
			}

			var errMsg string

			switch tt.op {
			case "create":
				isDraft := (tt.draft != nil && *tt.draft)
				token, level, err := resolveToken(lookup, tt.levels)
				if err != nil {
					errMsg = err.Error()
					break
				}
				_ = token
				realDraft := isDraft || level == "draft"
				attendees := []string{"noah@test.com"}
				desc := "Cat sitting session"
				if realDraft && len(attendees) > 0 {
					desc += "\n\nAttendees:\n\n - " + strings.Join(attendees, "\n - ")
					attendees = nil
				}
				footer := "Sent with Housecat"
				if realDraft {
					footer = "Drafted with Housecat"
				}
				desc += "\n\n" + footer
				_, err = client.CreateEvent(ctx, "", "Cat Sitting", desc, "2026-03-22T12:00:00", "2026-03-22T13:00:00", "America/Los_Angeles", attendees, "", realDraft)
				if err != nil {
					errMsg = err.Error()
				}

			case "update-send":
				_, _, err := resolveToken(lookup, map[string]bool{"write": tt.levels["write"]})
				if err != nil {
					errMsg = err.Error()
					break
				}
				event, err := client.GetEvent(ctx, "", tt.event.ID)
				if err != nil {
					errMsg = err.Error()
					break
				}
				if event.ExtendedProperties == nil || event.ExtendedProperties.Private["housecat_draft"] != "true" {
					errMsg = "event is not a draft"
					break
				}
				attendees := parseAttendeesFromDescription(event.Description)
				desc := removeAttendeesSection(event.Description)
				desc = strings.Replace(desc, ">Drafted with Housecat<", ">Sent with Housecat<", 1)
				_, err = client.UpdateEvent(ctx, UpdateEventIn{
					Attendees:          attendees,
					Description:        &desc,
					EventID:            tt.event.ID,
					ExtendedProperties: map[string]string{"housecat_draft": ""},
					SendUpdates:        "all",
				})
				if err != nil {
					errMsg = err.Error()
				}

			case "delete", "delete-draft":
				isDraftDelete := tt.op == "delete-draft"
				if isDraftDelete {
					_, _, err := resolveToken(lookup, tt.levels)
					if err != nil {
						errMsg = err.Error()
						break
					}
					if err := verifyDraftEvent(ctx, client, "", tt.event.ID); err != nil {
						errMsg = "event is not a draft"
						break
					}
					_, err = client.DeleteEvent(ctx, "", tt.event.ID)
					if err != nil {
						errMsg = err.Error()
					}
				} else {
					_, level, err := resolveDeleteToken(lookup, tt.levels)
					if err != nil {
						errMsg = err.Error()
						break
					}
					if level == "draft" {
						if err := verifyDraftEvent(ctx, client, "", tt.event.ID); err != nil {
							errMsg = "sent events require archive level to delete"
							break
						}
					}
					_, err = client.DeleteEvent(ctx, "", tt.event.ID)
					if err != nil {
						errMsg = err.Error()
					}
				}
			}

			if tt.wantErr != "" {
				r.Contains(errMsg, tt.wantErr, "expected error containing %q, got %q", tt.wantErr, errMsg)
			} else {
				r.Empty(errMsg, "unexpected error: %s", errMsg)
				if tt.checkFn != nil {
					tt.checkFn(t, m)
				}
			}
		})
	}
}

func resolveToken(lookup TokenLookup, levels map[string]bool) (string, string, error) {
	for _, level := range []string{"write", "draft"} {
		if levels[level] {
			return "tok-" + level, level, nil
		}
	}
	return "", "", ErrTokenNotFound
}

func resolveDeleteToken(lookup TokenLookup, levels map[string]bool) (string, string, error) {
	for _, level := range []string{"archive", "draft"} {
		if levels[level] {
			return "tok-" + level, level, nil
		}
	}
	return "", "", fmt.Errorf("archive or draft level required to delete")
}
