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

func TestFinallyCanonicalReminderRules(t *testing.T) {
	e, err := setupTestEnv()
	require.NoError(t, err)
	token := humaTokenFor(t, &testuser1)
	rules := `[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"anchored","anchor":"deadline","value":30,"unit":"minutes","direction":"before"},{"id":"39082ea0-f135-494a-b73c-c32b77eb8f00","kind":"exact","at":"2026-10-20T17:30:00.125+08:00"}]`
	type task struct {
		ID              int64                    `json:"id"`
		Revision        int64                    `json:"revision"`
		Rules           []map[string]interface{} `json:"reminder_rules"`
		DeadlineHasTime bool                     `json:"deadline_has_time"`
		Title           string                   `json:"title"`
		Done            bool                     `json:"done"`
	}
	decode := func(data []byte) task { var out task; require.NoError(t, json.Unmarshal(data, &out)); return out }
	created := humaRequest(t, e, http.MethodPost, "/api/v2/finally/projects/1/tasks", `{"title":"Proposal","due_date":"2026-10-20T18:00:00Z","deadline_has_time":true,"reminder_rules":`+rules+`}`, token, "")
	require.Equal(t, 201, created.Code, created.Body.String())
	original := decode(created.Body.Bytes())
	assert.Len(t, original.Rules, 2)
	assert.True(t, original.DeadlineHasTime)
	path := fmt.Sprintf("/api/v2/finally/tasks/%d", original.ID)
	read := humaRequest(t, e, http.MethodGet, path, "", token, "")
	require.Equal(t, 200, read.Code)
	assert.Equal(t, original.Rules, decode(read.Body.Bytes()).Rules)
	patch := func(base int64, fields string, status int) task {
		response := humaRequest(t, e, http.MethodPut, path, fmt.Sprintf(`{"base_revision":%d,"client":"phone","fields":%s}`, base, fields), token, "")
		require.Equal(t, status, response.Code, response.Body.String())
		if status != 200 {
			return task{}
		}
		return decode(response.Body.Bytes())
	}
	moved := patch(original.Revision, `{"due_date":"2026-10-21T00:00:00Z","deadline_has_time":false}`, 200)
	assert.False(t, moved.DeadlineHasTime)
	assert.Equal(t, original.Rules, moved.Rules)
	// Deadline precision and its value share field ownership.
	stale := humaRequest(t, e, http.MethodPut, path, fmt.Sprintf(`{"base_revision":%d,"client":"api","fields":{"deadline_has_time":true}}`, original.Revision), token, "")
	assert.Equal(t, 409, stale.Code, stale.Body.String())
	title := patch(moved.Revision, `{"title":"Retain rules"}`, 200)
	assert.Equal(t, original.Rules, title.Rules)
	completed := patch(title.Revision, `{"done":true}`, 200)
	assert.True(t, completed.Done)
	assert.Equal(t, original.Rules, completed.Rules)
	for _, invalid := range []string{
		`null`,
		`[{"id":"71459f969b7a45e48ac3a87f94b8ed47","kind":"exact","at":"2026-10-20T17:30:00Z"}]`,
		`[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"exact","at":"2026-10-20T17:30:00Z"},{"id":"71459F96-9B7A-45E4-8AC3-A87F94B8ED47","kind":"exact","at":"2026-10-21T17:30:00Z"}]`,
		`[{"id":"invalid","kind":"exact","at":"2026-10-20T17:30:00Z"}]`,
		`[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"anchored","anchor":"deadline","value":60,"unit":"minutes","direction":"before"}]`,
		`[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"exact","at":"2026-10-20T17:30:00Z","anchor":"deadline"}]`,
		`[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"exact","at":"2026-10-20T17:30:00Z"},{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"exact","at":"2026-10-21T17:30:00Z"}]`,
	} {
		patch(completed.Revision, `{"reminder_rules":`+invalid+`}`, 422)
	}
	patch(completed.Revision, `{"deadline_has_time":null}`, 422)
	cleared := patch(completed.Revision, `{"reminder_rules":[]}`, 200)
	assert.Empty(t, cleared.Rules)
	assert.NotNil(t, cleared.Rules)
	list := humaRequest(t, e, http.MethodGet, "/api/v2/finally/projects/1/tasks", "", token, "")
	require.Equal(t, 200, list.Code)
	var listed struct {
		Items []task `json:"items"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &listed))
	for _, item := range listed.Items {
		assert.NotNil(t, item.Rules)
	}
}
