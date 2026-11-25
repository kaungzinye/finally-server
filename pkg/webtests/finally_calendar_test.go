// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package webtests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/modules/keyvalue"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type googleCalendarDouble struct {
	server          *httptest.Server
	mu              sync.Mutex
	revoked         []string
	revokeStatus    int
	eventReads      []string
	eventPageTokens []string
	eventPageCounts map[string]int
	paginateEvents  bool
	eventPageCount  int
	refreshStarted  chan struct{}
	releaseRefresh  chan struct{}
	refreshOnce     sync.Once
	calendarCreated int
	eventRequests   []googleEventRequest
	// Each marker's status by event path. Google keeps a deleted event as cancelled.
	events             map[string]string
	deadlinesOnPageTwo bool
	listStarted        chan struct{}
	releaseList        chan struct{}
	listOnce           sync.Once
}

type googleEventRequest struct {
	Method string
	Path   string
	Body   string
}

func newGoogleCalendarDouble(t *testing.T) *googleCalendarDouble {
	t.Helper()
	double := &googleCalendarDouble{}
	double.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				http.Error(w, "invalid form", http.StatusBadRequest)
				return
			}
			if r.Form.Get("grant_type") == "refresh_token" {
				if double.refreshStarted != nil {
					double.refreshOnce.Do(func() { close(double.refreshStarted) })
					<-double.releaseRefresh
				}
				_, _ = w.Write([]byte(`{"access_token":"access-refreshed","expires_in":3600}`))
				return
			}
			code := r.Form.Get("code")
			expiresIn := 3600
			if code == "expired" {
				expiresIn = -60
			}
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-" + code,
				"refresh_token": "refresh-" + code,
				"expires_in":    expiresIn,
			}))
		case "/oauth2/v2/userinfo":
			code := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer access-")
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{
				"id":    "google-" + code,
				"email": code + "@example.com",
				"name":  code + " account",
			}))
		case "/revoke":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse revoke form: %v", err)
				http.Error(w, "invalid form", http.StatusBadRequest)
				return
			}
			double.mu.Lock()
			double.revoked = append(double.revoked, r.Form.Get("token"))
			double.mu.Unlock()
			if double.revokeStatus != 0 {
				http.Error(w, "revocation failed", double.revokeStatus)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/calendar/v3/calendars/primary/events":
			double.mu.Lock()
			authorization := r.Header.Get("Authorization")
			double.eventReads = append(double.eventReads, authorization)
			double.eventPageTokens = append(double.eventPageTokens, r.URL.Query().Get("pageToken"))
			if double.eventPageCounts == nil {
				double.eventPageCounts = make(map[string]int)
			}
			double.eventPageCounts[authorization]++
			pageNumber := double.eventPageCounts[authorization]
			double.mu.Unlock()
			if double.eventPageCount > 0 {
				nextPageToken := ""
				if pageNumber < double.eventPageCount {
					nextPageToken = fmt.Sprintf(`,"nextPageToken":"page-%d"`, pageNumber+1)
				}
				_, _ = fmt.Fprintf(w, `{"items":[]%s}`, nextPageToken)
				return
			}
			if double.paginateEvents && r.URL.Query().Get("pageToken") == "" {
				_, _ = w.Write([]byte(`{"nextPageToken":"page-2","items":[{"id":"event-1","summary":"First event","start":{"dateTime":"2026-07-16T09:00:00Z"},"end":{"dateTime":"2026-07-16T10:00:00Z"}}]}`))
				return
			}
			if double.paginateEvents {
				_, _ = w.Write([]byte(`{"items":[{"id":"event-2","summary":"Second event","start":{"dateTime":"2026-07-16T11:00:00Z"},"end":{"dateTime":"2026-07-16T12:00:00Z"}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"event-1","summary":"Private planning meeting","description":"Sensitive launch notes https://meet.example/private","location":"Private office","start":{"dateTime":"2026-07-16T09:00:00Z"},"end":{"dateTime":"2026-07-16T10:00:00Z"},"attendees":[{"email":"colleague@example.com","responseStatus":"accepted"}],"recurrence":["RRULE:FREQ=WEEKLY"]}]}`))
		case "/calendar/v3/users/me/calendarList":
			if double.listStarted != nil {
				double.listOnce.Do(func() { close(double.listStarted) })
				<-double.releaseList
			}
			if r.URL.Query().Get("minAccessRole") != "owner" {
				_, _ = w.Write([]byte(`{"items":[{"id":"shared-deadlines","summary":"Deadlines","accessRole":"reader"}]}`))
				return
			}
			if double.deadlinesOnPageTwo {
				if r.URL.Query().Get("pageToken") == "" {
					_, _ = w.Write([]byte(`{"nextPageToken":"page-2","items":[]}`))
				} else {
					_, _ = w.Write([]byte(`{"items":[{"id":"deadlines-calendar","summary":"Deadlines"}]}`))
				}
				return
			}
			double.mu.Lock()
			created := double.calendarCreated > 0
			double.mu.Unlock()
			if created {
				_, _ = w.Write([]byte(`{"items":[{"id":"deadlines-calendar","summary":"Deadlines"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/calendar/v3/calendars":
			double.mu.Lock()
			double.calendarCreated++
			double.mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"deadlines-calendar","summary":"Deadlines"}`))
		default:
			if strings.HasPrefix(r.URL.Path, "/calendar/v3/calendars/deadlines-calendar/events") {
				body, _ := io.ReadAll(r.Body)
				double.mu.Lock()
				double.eventRequests = append(double.eventRequests, googleEventRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
				if double.events == nil {
					double.events = map[string]string{}
				}
				var event struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				}
				_ = json.Unmarshal(body, &event)
				path := r.URL.Path
				if r.Method == http.MethodPost {
					path += "/" + event.ID
				}
				status, exists := double.events[path]
				code := http.StatusOK
				switch {
				case r.Method == http.MethodPost && exists:
					code = http.StatusConflict
				case r.Method == http.MethodPut && !exists:
					code = http.StatusNotFound
				case r.Method == http.MethodDelete && !exists:
					code = http.StatusNotFound
				case r.Method == http.MethodDelete && status == "cancelled":
					code = http.StatusGone
				case r.Method == http.MethodDelete:
					double.events[path] = "cancelled"
					code = http.StatusNoContent
				case event.Status != "":
					double.events[path] = event.Status
				}
				double.mu.Unlock()
				if code != http.StatusOK {
					w.WriteHeader(code)
					return
				}
				_, _ = w.Write([]byte(`{"id":"deadline-event"}`))
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(double.server.Close)
	return double
}

func TestFinallyCalendarReconcilesDeadlineMarkers(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	require.Equal(t, http.StatusCreated, connected.Code, "body: %s", connected.Body.String())
	created := humaRequest(t, e, http.MethodPost, "/api/v2/finally/projects/1/tasks", `{"title":"File taxes","due_date":"2026-09-14T00:00:00Z"}`, token, "")
	require.Equal(t, http.StatusCreated, created.Code, "body: %s", created.Body.String())
	var task struct {
		ID       int64 `json:"id"`
		Revision int64 `json:"revision"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &task))
	taskPath := fmt.Sprintf("/api/v2/finally/tasks/%d", task.ID)
	patch := func(fields string) {
		t.Helper()
		res := humaRequest(t, e, http.MethodPut, taskPath, fmt.Sprintf(`{"base_revision":%d,"client":"phone","fields":%s}`, task.Revision, fields), token, "")
		require.Equal(t, http.StatusOK, res.Code, "body: %s", res.Body.String())
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &task))
	}
	markerPath := fmt.Sprintf("/api/v2/finally/calendar/accounts/google-work/deadline-markers/%d", task.ID)

	dateOnly := humaRequest(t, e, http.MethodPut, markerPath, "", token, "")
	require.Equal(t, http.StatusNoContent, dateOnly.Code, "body: %s", dateOnly.Body.String())
	patch(`{"due_date":"2026-09-14T00:00:00Z","deadline_has_time":true}`)
	timed := humaRequest(t, e, http.MethodPut, markerPath, "", token, "")
	require.Equal(t, http.StatusNoContent, timed.Code, "body: %s", timed.Body.String())

	patch(`{"done":true}`)
	removed := humaRequest(t, e, http.MethodPut, markerPath, "", token, "")
	require.Equal(t, http.StatusNoContent, removed.Code, "body: %s", removed.Body.String())
	patch(`{"done":false}`)
	reopened := humaRequest(t, e, http.MethodPut, markerPath, "", token, "")
	require.Equal(t, http.StatusNoContent, reopened.Code, "body: %s", reopened.Body.String())
	double.mu.Lock()
	require.Len(t, double.events, 1)
	for _, status := range double.events {
		assert.Equal(t, "confirmed", status, "a reopened task's marker is visible again")
	}
	double.mu.Unlock()
	deleted := humaRequest(t, e, http.MethodDelete, fmt.Sprintf("%s?base_revision=%d&client=phone", taskPath, task.Revision), "", token, "")
	require.Equal(t, http.StatusNoContent, deleted.Code, "body: %s", deleted.Body.String())
	deletedMarker := humaRequest(t, e, http.MethodDelete, markerPath, "", token, "")
	require.Equal(t, http.StatusNoContent, deletedMarker.Code, "body: %s", deletedMarker.Body.String())

	double.mu.Lock()
	defer double.mu.Unlock()
	require.Equal(t, 1, double.calendarCreated)
	require.Len(t, double.eventRequests, 7)
	assert.Equal(t, http.MethodPost, double.eventRequests[0].Method)
	var dateOnlyEvent map[string]any
	require.NoError(t, json.Unmarshal([]byte(double.eventRequests[0].Body), &dateOnlyEvent))
	assert.NotEmpty(t, dateOnlyEvent["id"])
	delete(dateOnlyEvent, "id")
	assert.Equal(t, map[string]any{"status": "confirmed", "reminders": map[string]any{"useDefault": false}, "summary": "File taxes", "transparency": "transparent", "start": map[string]any{"date": "2026-09-14"}, "end": map[string]any{"date": "2026-09-15"}}, dateOnlyEvent)
	assert.Equal(t, http.MethodPost, double.eventRequests[1].Method)
	assert.Equal(t, http.MethodPut, double.eventRequests[2].Method)
	assert.JSONEq(t, `{"status":"confirmed","reminders":{"useDefault":false},"summary":"File taxes","transparency":"transparent","start":{"dateTime":"2026-09-14T00:00:00Z"},"end":{"dateTime":"2026-09-14T00:01:00Z"}}`, double.eventRequests[2].Body)
	assert.Equal(t, http.MethodDelete, double.eventRequests[3].Method)
	assert.Equal(t, double.eventRequests[2].Path, double.eventRequests[3].Path)
	assert.Equal(t, http.MethodPut, double.eventRequests[5].Method)
	assert.Contains(t, double.eventRequests[5].Body, `"status":"confirmed"`)
	assert.Equal(t, http.MethodDelete, double.eventRequests[6].Method)
	assert.Equal(t, double.eventRequests[2].Path, double.eventRequests[6].Path)
}

func TestFinallyCalendarDeadlineMarkersFollowCalendarPages(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	double.deadlinesOnPageTwo = true
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)
	connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	require.Equal(t, http.StatusCreated, connected.Code, connected.Body.String())
	created := humaRequest(t, e, http.MethodPost, "/api/v2/finally/projects/1/tasks", `{"title":"File taxes","due_date":"2026-09-14T00:00:00Z"}`, token, "")
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var task struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &task))
	path := fmt.Sprintf("/api/v2/finally/calendar/accounts/google-work/deadline-markers/%d", task.ID)

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		res := humaRequest(t, e, method, path, "", token, "")
		require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	}

	double.mu.Lock()
	defer double.mu.Unlock()
	assert.Zero(t, double.calendarCreated)
	require.Len(t, double.events, 1)
	for _, status := range double.events {
		assert.Equal(t, "cancelled", status)
	}
}

func TestFinallyCalendarDeadlineMarkersLeaveConnectionsFree(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	double.listStarted = make(chan struct{})
	double.releaseList = make(chan struct{})
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)
	connect := func(code string) int {
		return humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", fmt.Sprintf(`{"authorization_code":%q,"redirect_uri":"finally://oauth"}`, code), token, "").Code
	}
	require.Equal(t, http.StatusCreated, connect("work"))
	created := humaRequest(t, e, http.MethodPost, "/api/v2/finally/projects/1/tasks", `{"title":"File taxes","due_date":"2026-09-14T00:00:00Z"}`, token, "")
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var task struct {
		ID int64 `json:"id"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &task))
	path := fmt.Sprintf("/api/v2/finally/calendar/accounts/google-work/deadline-markers/%d", task.ID)
	published := make(chan int, 1)

	go func() { published <- humaRequest(t, e, http.MethodPut, path, "", token, "").Code }()
	<-double.listStarted
	// Publication waiting on Google leaves account connections free.
	connected := make(chan int, 1)
	go func() { connected <- connect("home") }()
	select {
	case code := <-connected:
		assert.Equal(t, http.StatusCreated, code)
	case <-time.After(5 * time.Second):
		t.Error("connecting an account waited for publication")
	}
	close(double.releaseList)
	assert.Equal(t, http.StatusNoContent, <-published)
}

func TestFinallyCalendarDeadlineMarkersRequireTaskAndAccountAccess(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	path := "/api/v2/finally/calendar/accounts/google-work/deadline-markers/1"

	unauthenticated := humaRequest(t, e, http.MethodPut, path, "", "", "")
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.Code, "body: %s", unauthenticated.Body.String())

	forbidden := humaRequest(t, e, http.MethodPut, path, "", humaTokenFor(t, &testuser15), "")
	assert.Equal(t, http.StatusForbidden, forbidden.Code, "body: %s", forbidden.Body.String())

	missingAccount := humaRequest(t, e, http.MethodPut, path, "", humaTokenFor(t, &testuser1), "")
	assert.Equal(t, http.StatusNotFound, missingAccount.Code, "body: %s", missingAccount.Body.String())
}

func configureGoogleCalendarDouble(t *testing.T, double *googleCalendarDouble) {
	t.Helper()
	config.ServiceTestingtoken.Set("calendar-test")
	config.KeyvalueType.Set("redis")
	config.CalendarGoogleClientID.Set("client-id")
	config.CalendarGoogleClientSecret.Set("client-secret")
	config.CalendarEncryptionKey.Set("calendar-encryption-key")
	config.CalendarGoogleTokenURL.Set(double.server.URL + "/token")
	config.CalendarGoogleAPIURL.Set(double.server.URL)
	config.CalendarGoogleRevokeURL.Set(double.server.URL + "/revoke")
	config.OutgoingRequestsAllowNonRoutableIPs.Set(true)
	t.Cleanup(func() {
		config.ServiceTestingtoken.Set("")
		config.KeyvalueType.Set("memory")
		config.CalendarGoogleClientID.Set("")
		config.CalendarGoogleClientSecret.Set("")
		config.CalendarEncryptionKey.Set("")
		config.OutgoingRequestsAllowNonRoutableIPs.Set(false)
	})
}

func TestFinallyCalendarReadsContextOnDemandAndRevocationStopsAccess(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	require.Equal(t, http.StatusCreated, connected.Code, "body: %s", connected.Body.String())

	contextBody := `{"account_ids":["google-work"],"from":"2026-07-16T00:00:00Z","to":"2026-07-17T00:00:00Z"}`
	contextRec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	require.Equal(t, http.StatusOK, contextRec.Code, "body: %s", contextRec.Body.String())
	assert.Contains(t, contextRec.Body.String(), "Private planning meeting")
	assert.Contains(t, contextRec.Body.String(), "Sensitive launch notes")
	assert.Contains(t, contextRec.Body.String(), "Private office")
	assert.Contains(t, contextRec.Body.String(), "2026-07-16T09:00:00Z")
	assert.Contains(t, contextRec.Body.String(), "colleague@example.com")
	assert.Contains(t, contextRec.Body.String(), "accepted")
	assert.Contains(t, contextRec.Body.String(), "RRULE:FREQ=WEEKLY")
	stored, exists, err := keyvalue.Get("finally_calendar_accounts_1")
	require.NoError(t, err)
	require.True(t, exists)
	serialized := fmt.Sprintf("%v", stored)
	assert.NotContains(t, serialized, "access-work")
	assert.NotContains(t, serialized, "refresh-work")
	assert.NotContains(t, serialized, "Sensitive launch notes")
	assert.NotContains(t, serialized, "colleague@example.com")

	revoked := humaRequest(t, e, http.MethodDelete, "/api/v2/finally/calendar/accounts/google-work", "", token, "")
	require.Equal(t, http.StatusNoContent, revoked.Code, "body: %s", revoked.Body.String())

	denied := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	assert.Equal(t, http.StatusNotFound, denied.Code, "body: %s", denied.Body.String())

	double.mu.Lock()
	defer double.mu.Unlock()
	assert.Equal(t, []string{"Bearer access-work"}, double.eventReads)
	assert.Equal(t, []string{"refresh-work"}, double.revoked)
}

func TestFinallyCalendarFailedGoogleRevocationStillStopsLocalAccess(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	double.revokeStatus = http.StatusServiceUnavailable
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	require.Equal(t, http.StatusCreated, connected.Code, "body: %s", connected.Body.String())

	revoked := humaRequest(t, e, http.MethodDelete, "/api/v2/finally/calendar/accounts/google-work", "", token, "")
	require.Equal(t, http.StatusBadGateway, revoked.Code, "body: %s", revoked.Body.String())
	listed := humaRequest(t, e, http.MethodGet, "/api/v2/finally/calendar/accounts", "", token, "")
	require.Equal(t, http.StatusOK, listed.Code, "body: %s", listed.Body.String())
	assert.JSONEq(t, `[]`, listed.Body.String())

	contextBody := `{"account_ids":["google-work"],"from":"2026-07-16T00:00:00Z","to":"2026-07-17T00:00:00Z"}`
	denied := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	assert.Equal(t, http.StatusNotFound, denied.Code, "body: %s", denied.Body.String())
}

func TestFinallyCalendarReadsEveryGoogleEventsPage(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	double.paginateEvents = true
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	require.Equal(t, http.StatusCreated, connected.Code, "body: %s", connected.Body.String())

	contextBody := `{"account_ids":["google-work"],"from":"2026-07-16T00:00:00Z","to":"2026-07-17T00:00:00Z"}`
	contextRec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	require.Equal(t, http.StatusOK, contextRec.Code, "body: %s", contextRec.Body.String())
	assert.Contains(t, contextRec.Body.String(), "First event")
	assert.Contains(t, contextRec.Body.String(), "Second event")

	double.mu.Lock()
	defer double.mu.Unlock()
	assert.Equal(t, []string{"", "page-2"}, double.eventPageTokens)
}

func TestFinallyCalendarStopsGooglePaginationAtTenPages(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	double.eventPageCount = 11
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	require.Equal(t, http.StatusCreated, connected.Code, "body: %s", connected.Body.String())

	contextBody := `{"account_ids":["google-work"],"from":"2026-07-16T00:00:00Z","to":"2026-07-17T00:00:00Z"}`
	contextRec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	require.Equal(t, http.StatusBadGateway, contextRec.Code, "body: %s", contextRec.Body.String())

	double.mu.Lock()
	defer double.mu.Unlock()
	assert.Len(t, double.eventPageTokens, 10)
}

func TestFinallyCalendarSharesTenPageBudgetAcrossAccounts(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	double.eventPageCount = 6
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	for _, code := range []string{"personal", "work"} {
		connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", fmt.Sprintf(`{"authorization_code":%q,"redirect_uri":"finally://oauth"}`, code), token, "")
		require.Equal(t, http.StatusCreated, connected.Code, "body: %s", connected.Body.String())
	}

	contextBody := `{"account_ids":["google-personal","google-work"],"from":"2026-07-16T00:00:00Z","to":"2026-07-17T00:00:00Z"}`
	contextRec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	require.Equal(t, http.StatusBadGateway, contextRec.Code, "body: %s", contextRec.Body.String())

	double.mu.Lock()
	defer double.mu.Unlock()
	assert.Len(t, double.eventPageTokens, 10)
}

func TestFinallyCalendarRequiresDurableStoreOutsideTests(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	config.ServiceTestingtoken.Set("")
	config.KeyvalueType.Set("memory")
	token := humaTokenFor(t, &testuser1)

	rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "keyvalue.type=redis")
}

func TestFinallyCalendarTestingTokenDoesNotBypassDurableStore(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	config.KeyvalueType.Set("memory")
	token := humaTokenFor(t, &testuser1)

	rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "keyvalue.type=redis")
}

func TestFinallyCalendarRequiresStableEncryptionKey(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	config.CalendarEncryptionKey.Set("")
	token := humaTokenFor(t, &testuser1)

	rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, token, "")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "calendar.encryptionkey")
}

func TestFinallyCalendarRejectsPlanningWindowsLongerThanThirtyOneDays(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	contextBody := `{"account_ids":["google-work"],"from":"2026-07-01T00:00:00Z","to":"2026-08-02T00:00:00Z"}`
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "31 days")
}

func TestFinallyCalendarRejectsMoreThanTenPlanningAccounts(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	contextBody := `{"account_ids":["1","2","3","4","5","6","7","8","9","10","11"],"from":"2026-07-01T00:00:00Z","to":"2026-07-02T00:00:00Z"}`
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "10 accounts")
}

func TestFinallyCalendarRejectsDuplicatePlanningAccounts(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	contextBody := `{"account_ids":["google-work","google-work"],"from":"2026-07-01T00:00:00Z","to":"2026-07-02T00:00:00Z"}`
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	assert.Equal(t, http.StatusBadRequest, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "duplicate account")
}

func TestFinallyCalendarRejectsPlanningBodiesLargerThanSixteenKiB(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	contextBody := fmt.Sprintf(`{"account_ids":[%q],"from":"2026-07-01T00:00:00Z","to":"2026-07-02T00:00:00Z"}`, strings.Repeat("x", 17<<10))
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "body: %s", rec.Body.String())
}

func TestFinallyCalendarRejectsConnectBodiesLargerThanSixteenKiB(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	connectBody := fmt.Sprintf(`{"authorization_code":%q,"redirect_uri":"finally://oauth"}`, strings.Repeat("x", 17<<10))
	rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", connectBody, token, "")
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code, "body: %s", rec.Body.String())
}

func TestFinallyCalendarBoundsConnectFields(t *testing.T) {
	tests := map[string]string{
		"authorization code": fmt.Sprintf(`{"authorization_code":%q,"redirect_uri":"finally://oauth"}`, strings.Repeat("x", 8193)),
		"redirect URI":       fmt.Sprintf(`{"authorization_code":"work","redirect_uri":%q}`, strings.Repeat("x", 2049)),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			e, err := setupTestEnv()
			require.NoError(t, err)
			double := newGoogleCalendarDouble(t)
			configureGoogleCalendarDouble(t, double)
			token := humaTokenFor(t, &testuser1)

			rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", body, token, "")
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
		})
	}
}

func TestFinallyCalendarRefreshesExpiredCredentials(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"expired","redirect_uri":"finally://oauth"}`, token, "")
	require.Equal(t, http.StatusCreated, connected.Code, "body: %s", connected.Body.String())
	contextBody := `{"account_ids":["google-expired"],"from":"2026-07-16T00:00:00Z","to":"2026-07-17T00:00:00Z"}`
	contextRec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	require.Equal(t, http.StatusOK, contextRec.Code, "body: %s", contextRec.Body.String())

	double.mu.Lock()
	defer double.mu.Unlock()
	assert.Equal(t, []string{"Bearer access-refreshed"}, double.eventReads)
}

func TestFinallyCalendarRefreshDoesNotBlockAnotherUser(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	double.refreshStarted = make(chan struct{})
	double.releaseRefresh = make(chan struct{})
	configureGoogleCalendarDouble(t, double)
	userOneToken := humaTokenFor(t, &testuser1)
	userTwoToken := humaTokenFor(t, &testuser2)

	expired := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"expired","redirect_uri":"finally://oauth"}`, userOneToken, "")
	require.Equal(t, http.StatusCreated, expired.Code, "body: %s", expired.Body.String())
	connected := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", `{"authorization_code":"work","redirect_uri":"finally://oauth"}`, userTwoToken, "")
	require.Equal(t, http.StatusCreated, connected.Code, "body: %s", connected.Body.String())

	contextDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		contextDone <- humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", `{"account_ids":["google-expired"],"from":"2026-07-16T00:00:00Z","to":"2026-07-17T00:00:00Z"}`, userOneToken, "")
	}()

	select {
	case <-double.refreshStarted:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	released := false
	defer func() {
		if !released {
			close(double.releaseRefresh)
		}
	}()

	revokeDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		revokeDone <- humaRequest(t, e, http.MethodDelete, "/api/v2/finally/calendar/accounts/google-work", "", userTwoToken, "")
	}()

	select {
	case revoked := <-revokeDone:
		require.Equal(t, http.StatusNoContent, revoked.Code, "body: %s", revoked.Body.String())
	case <-time.After(250 * time.Millisecond):
		t.Fatal("another user's account revoke was blocked by token refresh")
	}

	close(double.releaseRefresh)
	released = true
	refreshed := <-contextDone
	require.Equal(t, http.StatusOK, refreshed.Code, "body: %s", refreshed.Body.String())
}

func TestFinallyCalendarConnectsMultipleAccounts(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	double := newGoogleCalendarDouble(t)
	configureGoogleCalendarDouble(t, double)
	token := humaTokenFor(t, &testuser1)

	for _, code := range []string{"personal", "work"} {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/accounts", fmt.Sprintf(`{"authorization_code":%q,"redirect_uri":"finally://oauth"}`, code), token, "")
		require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())
		assert.NotContains(t, rec.Body.String(), "access-")
		assert.NotContains(t, rec.Body.String(), "refresh-")
	}

	rec := humaRequest(t, e, http.MethodGet, "/api/v2/finally/calendar/accounts", "", token, "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var accounts []struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &accounts))
	assert.Equal(t, []struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}{{ID: "google-personal", Email: "personal@example.com"}, {ID: "google-work", Email: "work@example.com"}}, accounts)

	contextBody := `{"account_ids":["google-personal","google-work"],"from":"2026-07-16T00:00:00Z","to":"2026-07-17T00:00:00Z"}`
	contextRec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/calendar/context", contextBody, token, "")
	require.Equal(t, http.StatusOK, contextRec.Code, "body: %s", contextRec.Body.String())
	var events []struct {
		AccountID string `json:"account_id"`
	}
	require.NoError(t, json.Unmarshal(contextRec.Body.Bytes(), &events))
	assert.Equal(t, []struct {
		AccountID string `json:"account_id"`
	}{{AccountID: "google-personal"}, {AccountID: "google-work"}}, events)

	otherUser := humaRequest(t, e, http.MethodGet, "/api/v2/finally/calendar/accounts", "", humaTokenFor(t, &testuser2), "")
	require.Equal(t, http.StatusOK, otherUser.Code, "body: %s", otherUser.Body.String())
	assert.JSONEq(t, `[]`, otherUser.Body.String())

	unauthenticated := humaRequest(t, e, http.MethodGet, "/api/v2/finally/calendar/accounts", "", "", "")
	assert.Equal(t, http.StatusUnauthorized, unauthenticated.Code, "body: %s", unauthenticated.Body.String())
}
