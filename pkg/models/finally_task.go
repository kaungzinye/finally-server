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

package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

type FinallyFieldVersion struct {
	Revision int64  `json:"revision"`
	Client   string `json:"client"`
}

type FinallyTaskConflict struct {
	Current *Task
	Fields  []string
}

func (e *FinallyTaskConflict) Error() string {
	return "Refresh the task and review the conflicting fields"
}

func finallyConflictFields(t *Task, base int64, client string, fields []string) []string {
	conflicts := []string{}
	for _, field := range fields {
		version := t.FinallyFieldVersions[field]
		if base > t.FinallyRevision || (version.Revision > base && (client != "phone" || version.Client == "phone")) {
			conflicts = append(conflicts, field)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

// Claiming the revision before mutating fields makes the conditional check and the patch one transaction.
func (t *Task) claimFinallyRevision(s *xorm.Session, fields []string, client string) error {
	old := t.FinallyRevision
	t.FinallyRevision++
	if t.FinallyFieldVersions == nil {
		t.FinallyFieldVersions = map[string]FinallyFieldVersion{}
	}
	for _, field := range fields {
		t.FinallyFieldVersions[field] = FinallyFieldVersion{Revision: t.FinallyRevision, Client: client}
	}
	affected, err := s.ID(t.ID).Where("finally_revision = ?", old).Cols("finally_revision", "finally_field_versions").Update(t)
	if err != nil {
		return err
	}
	if affected != 1 {
		return &FinallyTaskConflict{Current: t, Fields: fields}
	}
	t.finallyRevisionClaimed = true
	return nil
}

func (t *Task) PatchFinally(s *xorm.Session, a web.Auth, base int64, client string, patch map[string]json.RawMessage) error {
	if err := t.ReadOne(s, a); err != nil {
		return err
	}
	fields := make([]string, 0, len(patch))
	for field := range patch {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	if len(fields) == 0 {
		return InvalidFieldErrorWithMessage([]string{"fields"}, "A patch must include at least one field")
	}
	conflicts := finallyConflictFields(t, base, client, fields)
	if len(conflicts) > 0 {
		return &FinallyTaskConflict{Current: t, Fields: conflicts}
	}
	core := []string{}
	for _, field := range fields {
		raw := patch[field]
		if string(raw) == "null" && field != "start_date" && field != "due_date" {
			return InvalidFieldErrorWithMessage([]string{field}, "This field requires a value")
		}
		var err error
		switch field {
		case "title":
			err = json.Unmarshal(raw, &t.Title)
			if strings.TrimSpace(t.Title) == "" {
				err = fmt.Errorf("title must contain text")
			}
			core = append(core, field)
		case "done":
			err = json.Unmarshal(raw, &t.Done)
			core = append(core, field)
		case "priority":
			err = json.Unmarshal(raw, &t.Priority)
			if t.Priority < 0 || t.Priority > 4 {
				err = fmt.Errorf("priority must be between 0 and 4")
			}
			core = append(core, field)
		case "start_date":
			t.StartDate = time.Time{}
			err = json.Unmarshal(raw, &t.StartDate)
			core = append(core, field)
		case "due_date":
			t.DueDate = time.Time{}
			err = json.Unmarshal(raw, &t.DueDate)
			core = append(core, field)
		default:
			err = fmt.Errorf("unsupported task field")
		}
		if err != nil {
			return InvalidFieldErrorWithMessage([]string{field}, err.Error())
		}
	}
	if err := t.claimFinallyRevision(s, fields, client); err != nil {
		return err
	}
	if len(core) > 0 {
		if err := t.updateSingleTask(s, a, core); err != nil {
			return err
		}
	}
	return t.ReadOne(s, a)
}

func (t *Task) DeleteFinally(s *xorm.Session, a web.Auth, base int64, client string) error {
	if err := t.ReadOne(s, a); err != nil {
		return err
	}
	if base != t.FinallyRevision {
		return &FinallyTaskConflict{Current: t, Fields: []string{"deleted"}}
	}
	if err := t.claimFinallyRevision(s, []string{"deleted"}, client); err != nil {
		return err
	}
	return t.Delete(s, a)
}
