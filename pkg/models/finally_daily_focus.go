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
	"fmt"
	"time"

	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

const (
	FinallyDailyFocusDefaultLimit = 3
	FinallyDailyFocusMaxLimit     = 5
)

// FinallyDailyFocusPick is an opaque reference into a task provider's workspace.
// The server never resolves it, so one Daily Focus can mix Notion and Finally Server tasks.
type FinallyDailyFocusPick struct {
	Provider       string `json:"provider" minLength:"1" doc:"The task provider that owns the task, for example finally-server or notion."`
	WorkspaceID    string `json:"workspace_id" minLength:"1" doc:"The provider workspace the task lives in."`
	ExternalTaskID string `json:"external_task_id" minLength:"1" doc:"The task id inside the provider workspace."`
}

// FinallyDailyFocus is the few tasks picked for one day in a project, bounded by the focus limit.
type FinallyDailyFocus struct {
	ID         int64                   `xorm:"bigint autoincr not null unique pk" json:"-"`
	ProjectID  int64                   `xorm:"bigint not null unique(finally_daily_focus_project_day)" json:"project_id" readOnly:"true" doc:"The project this Daily Focus belongs to. Set from the URL."`
	Day        string                  `xorm:"varchar(10) not null unique(finally_daily_focus_project_day)" json:"day" readOnly:"true" doc:"The calendar day as YYYY-MM-DD. Set from the URL."`
	Picks      []FinallyDailyFocusPick `xorm:"JSON not null" json:"picks" doc:"The picks in display order, zero to focus_limit entries with no duplicates."`
	Confirmed  bool                    `xorm:"not null default false" json:"confirmed" doc:"Whether the user confirmed the Daily Focus for the day."`
	FocusLimit int                     `xorm:"int not null" json:"focus_limit" doc:"The bound on Daily Focus size, one to five. Defaults to three when omitted or zero."`
	Created    time.Time               `xorm:"created not null" json:"created" readOnly:"true" doc:"When this Daily Focus was first stored."`
	Updated    time.Time               `xorm:"updated not null" json:"updated" readOnly:"true" doc:"When this Daily Focus was last replaced."`
}

func (*FinallyDailyFocus) TableName() string {
	return "finally_daily_focus"
}

func (d *FinallyDailyFocus) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	return (&Project{ID: d.ProjectID}).CanRead(s, a)
}

func (d *FinallyDailyFocus) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	return (&Project{ID: d.ProjectID}).CanWrite(s, a)
}

// GetFinallyDailyFocus loads the Daily Focus stored for one project and day.
func GetFinallyDailyFocus(s *xorm.Session, projectID int64, day string) (*FinallyDailyFocus, error) {
	focus := &FinallyDailyFocus{}
	exists, err := s.Where("project_id = ? AND day = ?", projectID, day).Get(focus)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrFinallyDailyFocusDoesNotExist{ProjectID: projectID, Day: day}
	}
	if focus.Picks == nil {
		focus.Picks = []FinallyDailyFocusPick{}
	}
	return focus, nil
}

// Replace stores d as the whole Daily Focus for its project and day, creating it when none exists.
func (d *FinallyDailyFocus) Replace(s *xorm.Session) error {
	if d.Picks == nil {
		d.Picks = []FinallyDailyFocusPick{}
	}
	if d.FocusLimit == 0 {
		d.FocusLimit = FinallyDailyFocusDefaultLimit
	}
	if err := d.validate(); err != nil {
		return err
	}

	existing := &FinallyDailyFocus{}
	exists, err := s.Where("project_id = ? AND day = ?", d.ProjectID, d.Day).Get(existing)
	if err != nil {
		return err
	}
	if exists {
		_, err = s.ID(existing.ID).Cols("picks", "confirmed", "focus_limit").Update(d)
	} else {
		_, err = s.Insert(d)
	}
	if err != nil {
		return err
	}

	// Timestamps come back from the database at column precision so PUT and GET agree byte for byte.
	stored, err := GetFinallyDailyFocus(s, d.ProjectID, d.Day)
	if err != nil {
		return err
	}
	*d = *stored
	return nil
}

func (d *FinallyDailyFocus) validate() error {
	if d.FocusLimit < 1 || d.FocusLimit > FinallyDailyFocusMaxLimit {
		return InvalidFieldErrorWithMessage([]string{"focus_limit"}, fmt.Sprintf("focus_limit must be between 1 and %d", FinallyDailyFocusMaxLimit))
	}
	if len(d.Picks) > d.FocusLimit {
		return InvalidFieldErrorWithMessage([]string{"picks"}, fmt.Sprintf("picks cannot exceed the focus limit of %d", d.FocusLimit))
	}
	seen := make(map[FinallyDailyFocusPick]struct{}, len(d.Picks))
	for _, pick := range d.Picks {
		if pick.Provider == "" || pick.WorkspaceID == "" || pick.ExternalTaskID == "" {
			return InvalidFieldErrorWithMessage([]string{"picks"}, "every pick needs a provider, workspace_id and external_task_id")
		}
		if _, duplicate := seen[pick]; duplicate {
			return InvalidFieldErrorWithMessage([]string{"picks"}, "picks cannot contain the same task twice")
		}
		seen[pick] = struct{}{}
	}
	return nil
}
