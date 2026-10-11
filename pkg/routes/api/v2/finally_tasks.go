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
	"encoding/json"
	"errors"
	"net/http"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web/handler"
	"github.com/danielgtaylor/huma/v2"
)

type finallyTaskWriteInput struct {
	ID   int64 `path:"projecttask"`
	Body struct {
		BaseRevision int64                      `json:"base_revision" required:"true" minimum:"0" doc:"Revision from the task read. Stale deletion and same-field writes require review."`
		Client       string                     `json:"client" required:"true" enum:"phone,api" doc:"Explicit phone edits take priority over concurrent API edits."`
		Fields       map[string]json.RawMessage `json:"fields" required:"true" doc:"Narrow field patch. Omitted fields retain their current values; null clears task dates."`
	}
}

type finallyTaskConditionalInput struct {
	ID           int64  `path:"projecttask"`
	BaseRevision int64  `query:"base_revision" required:"true" minimum:"0" doc:"Revision from the task read."`
	Client       string `query:"client" enum:"phone,api" default:"api" doc:"The client submitting this conditional action."`
}

type finallyConflictResponse struct {
	huma.ErrorModel
	Current *models.Task `json:"current"`
	Fields  []string     `json:"conflicting_fields"`
}

func finallyTaskError(err error) error {
	var conflict *models.FinallyTaskConflict
	if errors.As(err, &conflict) {
		return &finallyConflictResponse{ErrorModel: huma.ErrorModel{Status: http.StatusConflict, Title: "Task conflict", Detail: conflict.Error()}, Current: conflict.Current, Fields: conflict.Fields}
	}
	return translateDomainError(err)
}

func finallyTasksCreate(ctx context.Context, in *struct {
	Project int64 `path:"project"`
	Body    models.Task
}) (*singleBody[models.Task], error) {
	in.Body.FinallyRevision = 1
	in.Body.FinallyFieldVersions = nil
	return tasksCreate(ctx, &struct {
		Project int64  `path:"project" doc:"The numeric id of the project to create the task in."`
		Format  string `query:"format" enum:"html,markdown" doc:"How rich-text fields are exchanged. See the API description."`
		Body    models.Task
	}{Project: in.Project, Body: in.Body})
}

func finallyTaskWrite(ctx context.Context, id int64, base int64, client string, fields map[string]json.RawMessage, deleting bool) (*models.Task, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	s := db.NewSession()
	defer s.Close()
	task := &models.Task{ID: id}
	var can bool
	if deleting {
		can, err = task.CanDelete(s, a)
	} else {
		can, err = task.CanUpdate(s, a)
	}
	if err != nil {
		return nil, translateDomainError(err)
	}
	if !can {
		return nil, huma.Error403Forbidden("forbidden")
	}
	if deleting {
		err = task.DeleteFinally(s, a, base, client)
	} else {
		err = task.PatchFinally(s, a, base, client, fields)
	}
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		var conflict *models.FinallyTaskConflict
		if errors.As(err, &conflict) {
			accepted := &models.Task{ID: id}
			if _, readErr := handler.DoReadOne(ctx, accepted, a); readErr != nil {
				return nil, translateDomainError(readErr)
			}
			conflict.Current = accepted
		}
		return nil, finallyTaskError(err)
	}
	if err = s.Commit(); err != nil {
		events.CleanupPending(s)
		return nil, err
	}
	events.DispatchPending(ctx, s)
	return task, nil
}

func finallyTasksUpdate(ctx context.Context, in *finallyTaskWriteInput) (*singleBody[models.Task], error) {
	task, err := finallyTaskWrite(ctx, in.ID, in.Body.BaseRevision, in.Body.Client, in.Body.Fields, false)
	if err != nil {
		return nil, err
	}
	return &singleBody[models.Task]{Body: task}, nil
}

func finallyTasksComplete(ctx context.Context, in *finallyTaskConditionalInput) (*singleBody[models.Task], error) {
	task, err := finallyTaskWrite(ctx, in.ID, in.BaseRevision, in.Client, map[string]json.RawMessage{"done": json.RawMessage("true")}, false)
	if err != nil {
		return nil, err
	}
	return &singleBody[models.Task]{Body: task}, nil
}

func finallyTasksDelete(ctx context.Context, in *finallyTaskConditionalInput) (*emptyBody, error) {
	_, err := finallyTaskWrite(ctx, in.ID, in.BaseRevision, in.Client, nil, true)
	if err != nil {
		return nil, err
	}
	return &emptyBody{}, nil
}
