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

package migration

import (
	"time"

	"src.techknowlogick.com/xormigrate"
	"xorm.io/xorm"
)

type finallyDailyFocusPick20260906171356 struct {
	Provider       string `json:"provider"`
	WorkspaceID    string `json:"workspace_id"`
	ExternalTaskID string `json:"external_task_id"`
}

type FinallyDailyFocus20260906171356 struct {
	ID         int64                                 `xorm:"bigint autoincr not null unique pk"`
	ProjectID  int64                                 `xorm:"bigint not null unique(finally_daily_focus_project_day)"`
	Day        string                                `xorm:"varchar(10) not null unique(finally_daily_focus_project_day)"`
	Picks      []finallyDailyFocusPick20260906171356 `xorm:"JSON not null"`
	Confirmed  bool                                  `xorm:"not null default false"`
	FocusLimit int                                   `xorm:"int not null"`
	Created    time.Time                             `xorm:"created not null"`
	Updated    time.Time                             `xorm:"updated not null"`
}

func (FinallyDailyFocus20260906171356) TableName() string {
	return "finally_daily_focus"
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20260906171356",
		Description: "Store the Finally Daily Focus per project and day",
		Migrate: func(tx *xorm.Engine) error {
			return tx.Sync(FinallyDailyFocus20260906171356{})
		},
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(FinallyDailyFocus20260906171356{})
		},
	})
}
