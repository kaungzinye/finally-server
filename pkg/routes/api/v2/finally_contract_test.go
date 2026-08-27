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

func TestFinallyClientContractRequiresSecuritySchemes(t *testing.T) {
	operation := &huma.Operation{}
	source := &huma.OpenAPI{
		Info: &huma.Info{},
		Paths: map[string]*huma.PathItem{
			"/finally/login":                        {Post: operation},
			"/finally/projects/{project}/tasks":     {Get: operation, Post: operation},
			"/finally/tasks/{projecttask}":          {Get: operation, Put: operation, Delete: operation},
			"/finally/tasks/{projecttask}/complete": {Post: operation},
		},
		Components: &huma.Components{SecuritySchemes: map[string]*huma.SecurityScheme{}},
	}

	_, err := finallyClientContract(source)
	require.ErrorContains(t, err, "required security schemes")
}
