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

	"github.com/google/uuid"
	"xorm.io/xorm"
)

type FinallyFieldVersion struct {
	Revision int64  `json:"revision"`
	Client   string `json:"client"`
}

type FinallyReminderRule struct {
	ID     string     `json:"id" doc:"Stable UUID identifying this rule across phones."`
	Kind   string     `json:"kind" enum:"deadline,exact" doc:"Deadline rules count down to the task's deadline. Exact rules fire once at a fixed instant."`
	Offset string     `json:"offset,omitempty" enum:"morningOf,daysBefore,weeksBefore,hourBefore" doc:"When a deadline rule fires relative to the deadline."`
	Value  *int       `json:"value,omitempty" doc:"Days for daysBefore (1 to 30) or weeks for weeksBefore (1 to 12)."`
	At     *time.Time `json:"at,omitempty" doc:"RFC3339 instant for an exact rule."`
}

// A zero maximum marks an offset that carries no value.
var finallyDeadlineOffsetMaxValues = map[string]int{"morningOf": 0, "daysBefore": 30, "weeksBefore": 12, "hourBefore": 0}

func validateFinallyReminderRules(rules []FinallyReminderRule) error {
	seen := map[string]bool{}
	deadlines := map[string]bool{}
	for _, rule := range rules {
		parsed, err := uuid.Parse(rule.ID)
		if err != nil || parsed.String() != strings.ToLower(rule.ID) || seen[parsed.String()] {
			return InvalidFieldErrorWithMessage([]string{"reminder_rules"}, "Each reminder rule needs a unique UUID")
		}
		seen[parsed.String()] = true
		switch rule.Kind {
		case "exact":
			if rule.At == nil || rule.At.IsZero() || rule.Offset != "" || rule.Value != nil {
				return InvalidFieldErrorWithMessage([]string{"reminder_rules"}, "Exact rules require only a fixed timestamp")
			}
		case "deadline":
			maxValue, known := finallyDeadlineOffsetMaxValues[rule.Offset]
			valueValid := rule.Value == nil
			if maxValue > 0 {
				valueValid = rule.Value != nil && *rule.Value >= 1 && *rule.Value <= maxValue
			}
			if !known || !valueValid || rule.At != nil {
				return InvalidFieldErrorWithMessage([]string{"reminder_rules"}, "Deadline rules require a known offset and its value range")
			}
			key := rule.Offset
			if rule.Value != nil {
				key = fmt.Sprintf("%s %d", rule.Offset, *rule.Value)
			}
			if deadlines[key] {
				return InvalidFieldErrorWithMessage([]string{"reminder_rules"}, "A task holds one deadline rule per offset and value")
			}
			deadlines[key] = true
		default:
			return InvalidFieldErrorWithMessage([]string{"reminder_rules"}, "Reminder kind must be deadline or exact")
		}
	}
	return nil
}

func ValidateFinallyTask(t *Task) error { return validateFinallyReminderRules(t.ReminderRules) }

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
		field = finallyFieldKey(field)
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
		t.FinallyFieldVersions[finallyFieldKey(field)] = FinallyFieldVersion{Revision: t.FinallyRevision, Client: client}
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
	metadata := []string{}
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
		case "deadline_has_time":
			err = json.Unmarshal(raw, &t.DeadlineHasTime)
			metadata = append(metadata, field)
		case "reminder_rules":
			// A fresh slice keeps stored rules from leaking fields into the replacement.
			t.ReminderRules = nil
			err = json.Unmarshal(raw, &t.ReminderRules)
			metadata = append(metadata, field)
		default:
			err = fmt.Errorf("unsupported task field")
		}
		if err != nil {
			return InvalidFieldErrorWithMessage([]string{field}, err.Error())
		}
	}
	if err := ValidateFinallyTask(t); err != nil {
		return err
	}
	if err := t.claimFinallyRevision(s, fields, client); err != nil {
		return err
	}
	if len(metadata) > 0 {
		if _, err := s.ID(t.ID).Cols(metadata...).Update(t); err != nil {
			return err
		}
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

func finallyFieldKey(field string) string {
	if field == "deadline_has_time" {
		return "due_date"
	}
	return field
}
