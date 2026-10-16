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
	rules := `[` +
		`{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"deadline","offset":"morningOf"},` +
		`{"id":"5d0f8f0e-3c47-4c1b-9a43-2f6f0d1f6a10","kind":"deadline","offset":"daysBefore","value":1},` +
		`{"id":"9e1c3a52-7b0d-4f6e-8d21-6a4b5c3e2f01","kind":"deadline","offset":"daysBefore","value":30},` +
		`{"id":"c2a7e4d9-1f35-4b80-a6c2-0e9d8b7f5a34","kind":"deadline","offset":"weeksBefore","value":12},` +
		`{"id":"e8b4f1a6-2d93-4c57-b0e1-7f3a9c6d4b28","kind":"deadline","offset":"hourBefore"},` +
		`{"id":"39082ea0-f135-494a-b73c-c32b77eb8f00","kind":"exact","at":"2026-10-20T15:00:00.125Z"}]`
	decodeRules := func(rules string) []map[string]interface{} {
		var out []map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(rules), &out))
		return out
	}
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
	assert.Equal(t, decodeRules(rules), original.Rules)
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
	// Reminder rules have their own field ownership, so the deadline move leaves a stale rule write clean.
	kept := humaRequest(t, e, http.MethodPut, path, fmt.Sprintf(`{"base_revision":%d,"client":"api","fields":{"reminder_rules":%s}}`, original.Revision, rules), token, "")
	require.Equal(t, 200, kept.Code, kept.Body.String())
	title := patch(decode(kept.Body.Bytes()).Revision, `{"title":"Retain rules"}`, 200)
	assert.Equal(t, original.Rules, title.Rules)
	completed := patch(title.Revision, `{"done":true}`, 200)
	assert.True(t, completed.Done)
	assert.Equal(t, original.Rules, completed.Rules)
	// A replacement stores exactly the sent shapes, whatever each position held before.
	replacement := `[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"exact","at":"2026-10-21T08:00:00Z"},{"id":"5d0f8f0e-3c47-4c1b-9a43-2f6f0d1f6a10","kind":"deadline","offset":"morningOf"},{"id":"9e1c3a52-7b0d-4f6e-8d21-6a4b5c3e2f01","kind":"deadline","offset":"weeksBefore","value":2}]`
	replaced := patch(completed.Revision, `{"reminder_rules":`+replacement+`}`, 200)
	assert.Equal(t, decodeRules(replacement), replaced.Rules)
	stale = humaRequest(t, e, http.MethodPut, path, fmt.Sprintf(`{"base_revision":%d,"client":"api","fields":{"reminder_rules":[]}}`, completed.Revision), token, "")
	assert.Equal(t, 409, stale.Code, stale.Body.String())
	assert.Contains(t, stale.Body.String(), `"conflicting_fields":["reminder_rules"]`)
	rule := func(fields string) string {
		return `[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47",` + fields + `}]`
	}
	for _, invalid := range []string{
		`null`,
		`[{"id":"71459f969b7a45e48ac3a87f94b8ed47","kind":"exact","at":"2026-10-20T17:30:00Z"}]`,
		`[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"exact","at":"2026-10-20T17:30:00Z"},{"id":"71459F96-9B7A-45E4-8AC3-A87F94B8ED47","kind":"exact","at":"2026-10-21T17:30:00Z"}]`,
		`[{"id":"invalid","kind":"exact","at":"2026-10-20T17:30:00Z"}]`,
		`[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"exact","at":"2026-10-20T17:30:00Z"},{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"exact","at":"2026-10-21T17:30:00Z"}]`,
		rule(`"kind":"anchored","offset":"daysBefore","value":1`),
		rule(`"kind":"deadline"`),
		rule(`"kind":"deadline","offset":"daysAfter","value":1`),
		rule(`"kind":"deadline","offset":"plannedDay"`),
		rule(`"kind":"deadline","offset":"daysBefore"`),
		rule(`"kind":"deadline","offset":"daysBefore","value":0`),
		rule(`"kind":"deadline","offset":"daysBefore","value":31`),
		rule(`"kind":"deadline","offset":"weeksBefore","value":0`),
		rule(`"kind":"deadline","offset":"weeksBefore","value":13`),
		rule(`"kind":"deadline","offset":"morningOf","value":1`),
		rule(`"kind":"deadline","offset":"hourBefore","value":1`),
		rule(`"kind":"deadline","offset":"morningOf","at":"2026-10-20T17:30:00Z"`),
		rule(`"kind":"exact"`),
		rule(`"kind":"exact","at":"2026-10-20T17:30:00Z","offset":"morningOf"`),
		rule(`"kind":"exact","at":"2026-10-20T17:30:00Z","value":1`),
		`[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"deadline","offset":"daysBefore","value":2},{"id":"5d0f8f0e-3c47-4c1b-9a43-2f6f0d1f6a10","kind":"deadline","offset":"daysBefore","value":2}]`,
		`[{"id":"71459f96-9b7a-45e4-8ac3-a87f94b8ed47","kind":"deadline","offset":"hourBefore"},{"id":"5d0f8f0e-3c47-4c1b-9a43-2f6f0d1f6a10","kind":"deadline","offset":"hourBefore"}]`,
	} {
		response := humaRequest(t, e, http.MethodPut, path, fmt.Sprintf(`{"base_revision":%d,"client":"phone","fields":{"reminder_rules":%s}}`, replaced.Revision, invalid), token, "")
		assert.Equal(t, 422, response.Code, invalid)
	}
	patch(replaced.Revision, `{"deadline_has_time":null}`, 422)
	cleared := patch(replaced.Revision, `{"reminder_rules":[]}`, 200)
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
