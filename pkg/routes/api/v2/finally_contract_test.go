// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package apiv2

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func finallyContractSourcePaths(operation *huma.Operation) map[string]*huma.PathItem {
	return map[string]*huma.PathItem{
		"/finally/login":                                {Post: operation},
		"/finally/projects":                             {Get: operation},
		"/finally/projects/{project}/tasks":             {Get: operation, Post: operation},
		"/finally/tasks/{projecttask}":                  {Get: operation, Put: operation, Delete: operation},
		"/finally/tasks/{projecttask}/complete":         {Post: operation},
		"/finally/calendar/accounts":                    {Get: operation, Post: operation},
		"/finally/calendar/accounts/{account}":          {Delete: operation},
		"/finally/calendar/context":                     {Post: operation},
		"/finally/projects/{project}/daily-focus/{day}": {Get: operation, Put: operation},
	}
}

func TestFinallyClientContractRequiresSecuritySchemes(t *testing.T) {
	source := &huma.OpenAPI{
		Info:       &huma.Info{},
		Paths:      finallyContractSourcePaths(&huma.Operation{}),
		Components: &huma.Components{SecuritySchemes: map[string]*huma.SecurityScheme{}},
	}

	_, err := finallyClientContract(source)
	require.ErrorContains(t, err, "required security schemes")
}

func TestFinallyClientContractRequiresDailyFocusOperations(t *testing.T) {
	paths := finallyContractSourcePaths(&huma.Operation{})
	paths["/finally/projects/{project}/daily-focus/{day}"] = &huma.PathItem{Get: &huma.Operation{}}
	source := &huma.OpenAPI{
		Info:  &huma.Info{},
		Paths: paths,
		Components: &huma.Components{SecuritySchemes: map[string]*huma.SecurityScheme{
			"JWTKeyAuth":   {},
			"APITokenAuth": {},
		}},
	}

	_, err := finallyClientContract(source)
	require.ErrorContains(t, err, "required lifecycle operation")
}
