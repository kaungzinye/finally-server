// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package webtests

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const finallyDailyFocusPath = "/api/v2/finally/projects/1/daily-focus/2026-09-06"

type dailyFocusPick struct {
	Provider       string `json:"provider"`
	WorkspaceID    string `json:"workspace_id"`
	ExternalTaskID string `json:"external_task_id"`
}

type dailyFocusResponse struct {
	ProjectID  int64            `json:"project_id"`
	Day        string           `json:"day"`
	Picks      []dailyFocusPick `json:"picks"`
	Confirmed  bool             `json:"confirmed"`
	FocusLimit int              `json:"focus_limit"`
	Created    string           `json:"created"`
	Updated    string           `json:"updated"`
}

func decodeDailyFocus(t *testing.T, body []byte) dailyFocusResponse {
	t.Helper()
	var record dailyFocusResponse
	require.NoError(t, json.Unmarshal(body, &record))
	return record
}

func TestFinallyDailyFocusReadBeforeAnyPickIsNotFound(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	rec := humaRequest(t, e, http.MethodGet, finallyDailyFocusPath, "", humaTokenFor(t, &testuser1), "")
	assert.Equal(t, http.StatusNotFound, rec.Code, "body: %s", rec.Body.String())
}

func TestFinallyDailyFocusRoundTripsPicksInOrderAcrossProviders(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	body := `{"picks":[
		{"provider":"finally-server","workspace_id":"1","external_task_id":"17"},
		{"provider":"notion","workspace_id":"ws-abc","external_task_id":"page-123"},
		{"provider":"finally-server","workspace_id":"1","external_task_id":"2"}
	],"confirmed":false,"focus_limit":3}`
	put := humaRequest(t, e, http.MethodPut, finallyDailyFocusPath, body, token, "")
	require.Equal(t, http.StatusOK, put.Code, "body: %s", put.Body.String())

	expectedPicks := []dailyFocusPick{
		{Provider: "finally-server", WorkspaceID: "1", ExternalTaskID: "17"},
		{Provider: "notion", WorkspaceID: "ws-abc", ExternalTaskID: "page-123"},
		{Provider: "finally-server", WorkspaceID: "1", ExternalTaskID: "2"},
	}
	stored := decodeDailyFocus(t, put.Body.Bytes())
	assert.Equal(t, int64(1), stored.ProjectID)
	assert.Equal(t, "2026-09-06", stored.Day)
	assert.Equal(t, expectedPicks, stored.Picks)
	assert.False(t, stored.Confirmed)
	assert.Equal(t, 3, stored.FocusLimit)
	assert.NotEmpty(t, stored.Created)
	assert.NotEmpty(t, stored.Updated)

	get := humaRequest(t, e, http.MethodGet, finallyDailyFocusPath, "", token, "")
	require.Equal(t, http.StatusOK, get.Code, "body: %s", get.Body.String())
	assert.Equal(t, stored, decodeDailyFocus(t, get.Body.Bytes()))
}

func TestFinallyDailyFocusSecondPutReplacesPicksAndConfirms(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	first := humaRequest(t, e, http.MethodPut, finallyDailyFocusPath, `{"picks":[
		{"provider":"finally-server","workspace_id":"1","external_task_id":"17"},
		{"provider":"notion","workspace_id":"ws-abc","external_task_id":"page-123"}
	],"confirmed":false,"focus_limit":3}`, token, "")
	require.Equal(t, http.StatusOK, first.Code, "body: %s", first.Body.String())

	second := humaRequest(t, e, http.MethodPut, finallyDailyFocusPath, `{"picks":[
		{"provider":"notion","workspace_id":"ws-abc","external_task_id":"page-123"}
	],"confirmed":true,"focus_limit":2}`, token, "")
	require.Equal(t, http.StatusOK, second.Code, "body: %s", second.Body.String())

	stored := decodeDailyFocus(t, second.Body.Bytes())
	assert.Equal(t, []dailyFocusPick{{Provider: "notion", WorkspaceID: "ws-abc", ExternalTaskID: "page-123"}}, stored.Picks)
	assert.True(t, stored.Confirmed)
	assert.Equal(t, 2, stored.FocusLimit)

	get := humaRequest(t, e, http.MethodGet, finallyDailyFocusPath, "", token, "")
	require.Equal(t, http.StatusOK, get.Code, "body: %s", get.Body.String())
	assert.Equal(t, stored, decodeDailyFocus(t, get.Body.Bytes()))
}

func TestFinallyDailyFocusEmptyPicksAndDefaultFocusLimit(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	put := humaRequest(t, e, http.MethodPut, finallyDailyFocusPath, `{"picks":[],"confirmed":false}`, token, "")
	require.Equal(t, http.StatusOK, put.Code, "body: %s", put.Body.String())
	stored := decodeDailyFocus(t, put.Body.Bytes())
	assert.Equal(t, []dailyFocusPick{}, stored.Picks)
	assert.Equal(t, 3, stored.FocusLimit)
}

func TestFinallyDailyFocusRejectsInvalidPicks(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	tests := []struct {
		name string
		body string
	}{
		{name: "focus limit above five", body: `{"picks":[],"confirmed":false,"focus_limit":6}`},
		{name: "focus limit below one", body: `{"picks":[],"confirmed":false,"focus_limit":-1}`},
		{name: "more picks than the focus limit", body: `{"picks":[
			{"provider":"finally-server","workspace_id":"1","external_task_id":"1"},
			{"provider":"finally-server","workspace_id":"1","external_task_id":"2"}
		],"confirmed":false,"focus_limit":1}`},
		{name: "duplicate pick", body: `{"picks":[
			{"provider":"notion","workspace_id":"ws-abc","external_task_id":"page-123"},
			{"provider":"notion","workspace_id":"ws-abc","external_task_id":"page-123"}
		],"confirmed":false,"focus_limit":3}`},
		{name: "empty provider", body: `{"picks":[{"provider":"","workspace_id":"1","external_task_id":"1"}],"confirmed":false,"focus_limit":3}`},
		{name: "empty workspace id", body: `{"picks":[{"provider":"notion","workspace_id":"","external_task_id":"1"}],"confirmed":false,"focus_limit":3}`},
		{name: "empty external task id", body: `{"picks":[{"provider":"notion","workspace_id":"1","external_task_id":""}],"confirmed":false,"focus_limit":3}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := humaRequest(t, e, http.MethodPut, finallyDailyFocusPath, tt.body, token, "")
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body: %s", rec.Body.String())
		})
	}

	get := humaRequest(t, e, http.MethodGet, finallyDailyFocusPath, "", token, "")
	assert.Equal(t, http.StatusNotFound, get.Code, "rejected picks must not be stored, body: %s", get.Body.String())
}

func TestFinallyDailyFocusRejectsMalformedDay(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	for _, day := range []string{"2026-9-6", "06-09-2026", "2026-13-01", "today", "2026-09-06T00:00:00Z"} {
		t.Run(day, func(t *testing.T) {
			path := "/api/v2/finally/projects/1/daily-focus/" + day
			get := humaRequest(t, e, http.MethodGet, path, "", token, "")
			assert.Equal(t, http.StatusUnprocessableEntity, get.Code, "body: %s", get.Body.String())
			put := humaRequest(t, e, http.MethodPut, path, `{"picks":[],"confirmed":false,"focus_limit":3}`, token, "")
			assert.Equal(t, http.StatusUnprocessableEntity, put.Code, "body: %s", put.Body.String())
		})
	}
}

func TestFinallyDailyFocusRequiresProjectPermission(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	t.Run("no access to the project", func(t *testing.T) {
		token := humaTokenFor(t, &testuser15)
		get := humaRequest(t, e, http.MethodGet, finallyDailyFocusPath, "", token, "")
		assert.Equal(t, http.StatusForbidden, get.Code, "body: %s", get.Body.String())
		put := humaRequest(t, e, http.MethodPut, finallyDailyFocusPath, `{"picks":[],"confirmed":false,"focus_limit":3}`, token, "")
		assert.Equal(t, http.StatusForbidden, put.Code, "body: %s", put.Body.String())
	})

	t.Run("read only access can read but not write", func(t *testing.T) {
		owner := humaTokenFor(t, &testuser6)
		path := "/api/v2/finally/projects/9/daily-focus/2026-09-06"
		seeded := humaRequest(t, e, http.MethodPut, path, `{"picks":[],"confirmed":false,"focus_limit":3}`, owner, "")
		require.Equal(t, http.StatusOK, seeded.Code, "body: %s", seeded.Body.String())

		reader := humaTokenFor(t, &testuser1)
		get := humaRequest(t, e, http.MethodGet, path, "", reader, "")
		assert.Equal(t, http.StatusOK, get.Code, "body: %s", get.Body.String())
		put := humaRequest(t, e, http.MethodPut, path, `{"picks":[],"confirmed":true,"focus_limit":3}`, reader, "")
		assert.Equal(t, http.StatusForbidden, put.Code, "body: %s", put.Body.String())
	})

	t.Run("requires authentication", func(t *testing.T) {
		get := humaRequest(t, e, http.MethodGet, finallyDailyFocusPath, "", "", "")
		assert.Equal(t, http.StatusUnauthorized, get.Code, "body: %s", get.Body.String())
	})
}

func TestFinallyDailyFocusContractExposesRoutes(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	rec := humaRequest(t, e, http.MethodGet, "/api/v2/finally/openapi.json", "", "", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var contract struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &contract))
	dailyFocus := contract.Paths["/finally/projects/{project}/daily-focus/{day}"]
	assert.Equal(t, "finally-daily-focus-read", dailyFocus["get"].OperationID)
	assert.Equal(t, "finally-daily-focus-replace", dailyFocus["put"].OperationID)
	assert.Len(t, dailyFocus, 2)
}
