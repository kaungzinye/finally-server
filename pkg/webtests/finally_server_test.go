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
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinallyServerOpenAPIContract(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	rec := humaRequest(t, e, http.MethodGet, "/api/v2/finally/openapi.json", "", "", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var contract struct {
		Info struct {
			Title string `json:"title"`
		} `json:"info"`
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
		Components struct {
			Schemas         map[string]json.RawMessage `json:"schemas"`
			SecuritySchemes map[string]struct {
				Description string `json:"description"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &contract))

	assert.Equal(t, "Finally Client API", contract.Info.Title)
	assert.Equal(t, map[string]map[string]struct {
		OperationID string `json:"operationId"`
	}{
		"/finally/login": {
			"post": {OperationID: "finally-auth-login"},
		},
		"/finally/projects": {
			"get": {OperationID: "finally-projects-list"},
		},
		"/finally/projects/{project}/tasks": {
			"get":  {OperationID: "finally-tasks-list"},
			"post": {OperationID: "finally-tasks-create"},
		},
		"/finally/tasks/{projecttask}": {
			"get":    {OperationID: "finally-tasks-read"},
			"put":    {OperationID: "finally-tasks-update"},
			"delete": {OperationID: "finally-tasks-delete"},
		},
		"/finally/tasks/{projecttask}/complete": {
			"post": {OperationID: "finally-tasks-complete"},
		},
		"/finally/calendar/accounts": {
			"get":  {OperationID: "finally-calendar-accounts-list"},
			"post": {OperationID: "finally-calendar-accounts-connect"},
		},
		"/finally/calendar/accounts/{account}": {
			"delete": {OperationID: "finally-calendar-accounts-revoke"},
		},
		"/finally/calendar/context": {
			"post": {OperationID: "finally-calendar-context-read"},
		},
	}, contract.Paths)
	assert.Equal(t, "User session JWT issued via /api/v2/finally/login.", contract.Components.SecuritySchemes["JWTKeyAuth"].Description)
	assert.Equal(t, "Vikunja API token (tk_ prefix) with scoped permissions. Created via /api/v2/tokens.", contract.Components.SecuritySchemes["APITokenAuth"].Description)
	assert.Len(t, contract.Components.SecuritySchemes, 2)
	assert.NotContains(t, contract.Components.Schemas, "AdminUser")
	assert.NotContains(t, contract.Components.Schemas, "LinkShareToken")
}

func TestFinallyServerProjectDiscovery(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	t.Run("login token discovers writable projects in the iOS response shape", func(t *testing.T) {
		login := humaRequest(t, e, http.MethodPost, "/api/v2/finally/login", `{"username":"user1","password":"12345678"}`, "", "")
		require.Equal(t, http.StatusOK, login.Code, "body: %s", login.Body.String())
		var session struct {
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(login.Body.Bytes(), &session))
		require.NotEmpty(t, session.Token)

		rec := humaRequest(t, e, http.MethodGet, "/api/v2/finally/projects", "", session.Token, "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

		var projects []map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &projects))
		projectTitles := make(map[int64]string, len(projects))
		for _, project := range projects {
			assert.Len(t, project, 2)
			assert.Contains(t, project, "id")
			assert.Contains(t, project, "title")
			var id int64
			var title string
			require.NoError(t, json.Unmarshal(project["id"], &id))
			require.NoError(t, json.Unmarshal(project["title"], &title))
			projectTitles[id] = title
		}
		assert.Equal(t, "Test1", projectTitles[1])
		assert.Equal(t, "Test10", projectTitles[10])
		assert.NotContains(t, projectTitles, int64(9))
	})

	t.Run("returns an empty array when every accessible project is read only", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/finally/projects", "", humaTokenFor(t, &testuser14), "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.JSONEq(t, `[]`, rec.Body.String())
	})

	t.Run("requires authentication", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodGet, "/api/v2/finally/projects", "", "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
	})
}

func TestFinallyServerFullOpenAPIUsesV2AuthenticationLinks(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	rec := humaRequest(t, e, http.MethodGet, "/api/v2/openapi.json", "", "", "")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var contract struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				Description string `json:"description"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &contract))

	assert.Equal(t, "User session JWT issued via /api/v2/login.", contract.Components.SecuritySchemes["JWTKeyAuth"].Description)
	assert.Equal(t, "Vikunja API token (tk_ prefix) with scoped permissions. Created via /api/v2/tokens.", contract.Components.SecuritySchemes["APITokenAuth"].Description)
	assert.Equal(t, "finally-contract", contract.Paths["/finally/openapi.json"]["get"].OperationID)
}

func TestFinallyServerAuthentication(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	t.Run("valid credentials issue a token", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/login", `{"username":"user1","password":"12345678"}`, "", "")
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"token":"`)
	})

	t.Run("invalid credentials are rejected", func(t *testing.T) {
		rec := humaRequest(t, e, http.MethodPost, "/api/v2/finally/login", `{"username":"user1","password":"wrong"}`, "", "")
		assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
	})
}

func TestFinallyServerAuthenticatedTaskLifecycle(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)

	type taskResponse struct {
		ID        int64  `json:"id"`
		ProjectID int64  `json:"project_id"`
		Title     string `json:"title"`
		Done      bool   `json:"done"`
		DoneAt    string `json:"done_at"`
	}
	decodeTask := func(recBody []byte) taskResponse {
		var task taskResponse
		require.NoError(t, json.Unmarshal(recBody, &task))
		return task
	}

	created := humaRequest(t, e, http.MethodPost, "/api/v2/finally/projects/1/tasks", `{"title":"Plan tomorrow"}`, token, "")
	require.Equal(t, http.StatusCreated, created.Code, "body: %s", created.Body.String())
	task := decodeTask(created.Body.Bytes())
	assert.Positive(t, task.ID)
	assert.Equal(t, int64(1), task.ProjectID)
	assert.Equal(t, "Plan tomorrow", task.Title)
	assert.False(t, task.Done)

	listed := humaRequest(t, e, http.MethodGet, "/api/v2/finally/projects/1/tasks", "", token, "")
	require.Equal(t, http.StatusOK, listed.Code, "body: %s", listed.Body.String())
	var taskList struct {
		Items []taskResponse `json:"items"`
		Total int64          `json:"total"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &taskList))
	assert.GreaterOrEqual(t, taskList.Total, int64(1))
	assert.Contains(t, taskList.Items, task)

	taskPath := fmt.Sprintf("/api/v2/finally/tasks/%d", task.ID)
	read := humaRequest(t, e, http.MethodGet, taskPath, "", token, "")
	require.Equal(t, http.StatusOK, read.Code, "body: %s", read.Body.String())
	assert.Equal(t, "Plan tomorrow", decodeTask(read.Body.Bytes()).Title)

	updated := humaRequest(t, e, http.MethodPut, taskPath, `{"title":"Plan focused tomorrow"}`, token, "")
	require.Equal(t, http.StatusOK, updated.Code, "body: %s", updated.Body.String())
	task = decodeTask(updated.Body.Bytes())
	assert.Equal(t, "Plan focused tomorrow", task.Title)
	assert.False(t, task.Done)

	completed := humaRequest(t, e, http.MethodPost, taskPath+"/complete", "", token, "")
	require.Equal(t, http.StatusOK, completed.Code, "body: %s", completed.Body.String())
	task = decodeTask(completed.Body.Bytes())
	assert.Equal(t, "Plan focused tomorrow", task.Title)
	assert.True(t, task.Done)
	assert.NotEmpty(t, task.DoneAt)

	deleted := humaRequest(t, e, http.MethodDelete, taskPath, "", token, "")
	require.Equal(t, http.StatusNoContent, deleted.Code, "body: %s", deleted.Body.String())
	assert.Empty(t, deleted.Body.String())

	missing := humaRequest(t, e, http.MethodGet, taskPath, "", token, "")
	assert.Equal(t, http.StatusNotFound, missing.Code, "body: %s", missing.Body.String())

	missingCompletion := humaRequest(t, e, http.MethodPost, taskPath+"/complete", "", token, "")
	assert.Equal(t, http.StatusNotFound, missingCompletion.Code, "body: %s", missingCompletion.Body.String())
}

func TestFinallyServerTaskLifecycleRequiresAuthentication(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/v2/finally/projects/1/tasks"},
		{name: "create", method: http.MethodPost, path: "/api/v2/finally/projects/1/tasks", body: `{"title":"Unauthorized"}`},
		{name: "read", method: http.MethodGet, path: "/api/v2/finally/tasks/1"},
		{name: "update", method: http.MethodPut, path: "/api/v2/finally/tasks/1", body: `{"title":"Unauthorized"}`},
		{name: "complete", method: http.MethodPost, path: "/api/v2/finally/tasks/1/complete"},
		{name: "delete", method: http.MethodDelete, path: "/api/v2/finally/tasks/1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := humaRequest(t, e, tt.method, tt.path, tt.body, "", "")
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "body: %s", rec.Body.String())
		})
	}
}

func TestFinallyServerTaskLifecycleRequiresPermission(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser15)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/v2/finally/projects/1/tasks"},
		{name: "create", method: http.MethodPost, path: "/api/v2/finally/projects/1/tasks", body: `{"title":"Forbidden"}`},
		{name: "read", method: http.MethodGet, path: "/api/v2/finally/tasks/1"},
		{name: "update", method: http.MethodPut, path: "/api/v2/finally/tasks/1", body: `{"title":"Forbidden"}`},
		{name: "complete", method: http.MethodPost, path: "/api/v2/finally/tasks/1/complete"},
		{name: "delete", method: http.MethodDelete, path: "/api/v2/finally/tasks/1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := humaRequest(t, e, tt.method, tt.path, tt.body, token, "")
			assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
		})
	}
}
