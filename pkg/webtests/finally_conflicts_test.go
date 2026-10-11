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

func TestFinallyPhonePriorityConflicts(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)
	create := humaRequest(t, e, http.MethodPost, "/api/v2/finally/projects/1/tasks", `{"title":"Finish proposal","due_date":"2026-10-20T16:00:00Z"}`, token, "")
	require.Equal(t, 201, create.Code, create.Body.String())
	type task struct {
		ID       int64  `json:"id"`
		Revision int64  `json:"revision"`
		Title    string `json:"title"`
		Deadline string `json:"due_date"`
		Done     bool   `json:"done"`
	}
	decode := func(body []byte) task { var out task; require.NoError(t, json.Unmarshal(body, &out)); return out }
	contract := humaRequest(t, e, http.MethodGet, "/api/v2/finally/openapi.json", "", "", "")
	require.Equal(t, 200, contract.Code)
	var schema map[string]interface{}
	require.NoError(t, json.Unmarshal(contract.Body.Bytes(), &schema))
	paths := schema["paths"].(map[string]interface{})
	for _, route := range []struct{ path, method string }{{"/finally/tasks/{projecttask}", "put"}, {"/finally/tasks/{projecttask}", "delete"}, {"/finally/tasks/{projecttask}/complete", "post"}} {
		operation := paths[route.path].(map[string]interface{})[route.method].(map[string]interface{})
		assert.NotEmpty(t, operation["description"])
		assert.Contains(t, operation["responses"], "409")
	}
	original := decode(create.Body.Bytes())
	require.EqualValues(t, 1, original.Revision)
	path := fmt.Sprintf("/api/v2/finally/tasks/%d", original.ID)
	patch := func(base int64, client, fields string, status int) task {
		rec := humaRequest(t, e, http.MethodPut, path, fmt.Sprintf(`{"base_revision":%d,"client":%q,"fields":%s}`, base, client, fields), token, "")
		require.Equal(t, status, rec.Code, rec.Body.String())
		if status == 409 {
			var conflict struct {
				Current task     `json:"current"`
				Fields  []string `json:"conflicting_fields"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &conflict))
			assert.NotEmpty(t, conflict.Fields)
			return conflict.Current
		}
		if status != 200 {
			return task{}
		}
		return decode(rec.Body.Bytes())
	}
	phone := patch(1, "phone", `{"due_date":"2026-10-20T18:00:00Z"}`, 200)
	assert.EqualValues(t, 2, phone.Revision)
	stale := patch(1, "api", `{"due_date":"2026-10-20T17:00:00Z"}`, 409)
	assert.Equal(t, phone, stale)
	title := patch(1, "api", `{"title":"Unrelated accepted edit"}`, 200)
	assert.Equal(t, phone.Deadline, title.Deadline)
	assert.Equal(t, "Unrelated accepted edit", title.Title)
	// A phone's same-field decision wins an API edit made after its read.
	phoneTitle := patch(2, "phone", `{"title":"Phone decision"}`, 200)
	assert.Equal(t, "Phone decision", phoneTitle.Title)
	otherPhone := patch(2, "phone", `{"title":"Other phone"}`, 409)
	assert.Equal(t, phoneTitle, otherPhone)
	completed := patch(phoneTitle.Revision, "phone", `{"done":true}`, 200)
	reopen := patch(phoneTitle.Revision, "api", `{"done":false}`, 409)
	assert.True(t, reopen.Done)
	reopened := patch(completed.Revision, "phone", `{"done":false}`, 200)
	assert.False(t, reopened.Done)
	patch(completed.Revision, "api", `{"done":true}`, 409)
	staleDelete := humaRequest(t, e, http.MethodDelete, path+fmt.Sprintf("?base_revision=%d&client=phone", completed.Revision), "", token, "")
	assert.Equal(t, 409, staleDelete.Code, staleDelete.Body.String())
	upstream := humaRequest(t, e, http.MethodPut, fmt.Sprintf("/api/v2/tasks/%d", original.ID), `{"title":"Blind overwrite"}`, token, "")
	assert.Equal(t, 422, upstream.Code, upstream.Body.String())
	for _, body := range []string{`{"client":"phone","fields":{"title":"Missing revision"}}`, `{"base_revision":0,"fields":{"title":"Missing client"}}`, `{"base_revision":0,"client":"phone"}`} {
		missing := humaRequest(t, e, http.MethodPut, path, body, token, "")
		assert.Equal(t, 422, missing.Code, missing.Body.String())
	}
	generated := humaRequest(t, e, http.MethodPatch, path, `{"title":"Blind patch"}`, token, "")
	assert.Equal(t, 405, generated.Code, generated.Body.String())
	for _, fields := range []string{`{"title":null}`, `{"title":"  "}`, `{"done":null}`, `{"priority":null}`, `{"priority":8}`, `{"unknown":true}`} {
		patch(reopened.Revision, "phone", fields, 422)
	}
	cleared := patch(reopened.Revision, "phone", `{"due_date":null}`, 200)
	assert.Contains(t, cleared.Deadline, "0001-01-01")
	deleted := humaRequest(t, e, http.MethodDelete, path+fmt.Sprintf("?base_revision=%d&client=phone", cleared.Revision), "", token, "")
	require.Equal(t, 204, deleted.Code, deleted.Body.String())
	patch(cleared.Revision, "phone", `{"title":"Resurrect"}`, 404)
}
