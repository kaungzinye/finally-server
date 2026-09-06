// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package apiv2

import (
	"context"
	"net/http"
	"time"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"

	"github.com/danielgtaylor/huma/v2"
)

const finallyDailyFocusDayLayout = "2006-01-02"

type finallyDailyFocusPath struct {
	ProjectID int64  `path:"project" doc:"The numeric id of the project."`
	Day       string `path:"day" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" doc:"The calendar day as YYYY-MM-DD."`
}

type finallyDailyFocusReplaceInput struct {
	ProjectID int64  `path:"project" doc:"The numeric id of the project."`
	Day       string `path:"day" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}$" doc:"The calendar day as YYYY-MM-DD."`
	Body      struct {
		Picks      []models.FinallyDailyFocusPick `json:"picks" doc:"The picks in display order, zero to focus_limit entries with no duplicates."`
		Confirmed  bool                           `json:"confirmed,omitempty" doc:"Whether the user confirmed the Daily Focus for the day."`
		FocusLimit int                            `json:"focus_limit,omitempty" doc:"The bound on Daily Focus size, one to five. Defaults to three when omitted or zero."`
	}
}

func RegisterFinallyDailyFocusRoutes(api huma.API) {
	tags := []string{"finally"}
	Register(api, huma.Operation{
		OperationID: "finally-daily-focus-read",
		Summary:     "Get a Daily Focus",
		Description: "Returns the Daily Focus stored for one project and day.",
		Method:      http.MethodGet,
		Path:        "/finally/projects/{project}/daily-focus/{day}",
		Tags:        tags,
	}, finallyDailyFocusRead)
	Register(api, huma.Operation{
		OperationID: "finally-daily-focus-replace",
		Summary:     "Replace a Daily Focus",
		Description: "Stores the whole Daily Focus for one project and day, creating it when none exists. Picks are opaque provider references and are never checked against server tasks.",
		Method:      http.MethodPut,
		Path:        "/finally/projects/{project}/daily-focus/{day}",
		Tags:        tags,
	}, finallyDailyFocusReplace)
}

func finallyDailyFocusRead(ctx context.Context, in *finallyDailyFocusPath) (*singleBody[models.FinallyDailyFocus], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := finallyDailyFocusDay(in.Day); err != nil {
		return nil, err
	}
	s := db.NewSession()
	defer s.Close()

	focus := &models.FinallyDailyFocus{ProjectID: in.ProjectID, Day: in.Day}
	can, _, err := focus.CanRead(s, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	if !can {
		return nil, huma.Error403Forbidden("forbidden")
	}
	focus, err = models.GetFinallyDailyFocus(s, in.ProjectID, in.Day)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.FinallyDailyFocus]{Body: focus}, nil
}

func finallyDailyFocusReplace(ctx context.Context, in *finallyDailyFocusReplaceInput) (*singleBody[models.FinallyDailyFocus], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := finallyDailyFocusDay(in.Day); err != nil {
		return nil, err
	}
	s := db.NewSession()
	defer s.Close()

	focus := &models.FinallyDailyFocus{
		ProjectID:  in.ProjectID,
		Day:        in.Day,
		Picks:      in.Body.Picks,
		Confirmed:  in.Body.Confirmed,
		FocusLimit: in.Body.FocusLimit,
	}
	can, err := focus.CanUpdate(s, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	if !can {
		return nil, huma.Error403Forbidden("forbidden")
	}
	if err := focus.Replace(s); err != nil {
		_ = s.Rollback()
		return nil, translateDomainError(err)
	}
	if err := s.Commit(); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.FinallyDailyFocus]{Body: focus}, nil
}

func finallyDailyFocusDay(raw string) error {
	day, err := time.Parse(finallyDailyFocusDayLayout, raw)
	if err != nil || day.Format(finallyDailyFocusDayLayout) != raw {
		return huma.Error422UnprocessableEntity("day must be a calendar date formatted as YYYY-MM-DD")
	}
	return nil
}
