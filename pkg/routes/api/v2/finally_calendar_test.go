// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package apiv2

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestFinallyCalendarErrorDoesNotMislabelInternalFailure(t *testing.T) {
	err := finallyCalendarError(errors.New("calendar storage failed"))
	statusError, ok := err.(huma.StatusError)
	require.True(t, ok)
	require.Equal(t, http.StatusInternalServerError, statusError.GetStatus())
}
