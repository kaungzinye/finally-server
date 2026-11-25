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

package calendar

import (
	"errors"
	"testing"

	"code.vikunja.io/api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinallyDeadlineReadsTheTaskWhilePublicationIsLocked(t *testing.T) {
	config.InitDefaultConfig()
	config.CalendarEncryptionKey.Set("calendar-encryption-key")
	config.CalendarGoogleClientID.Set("client-id")
	config.CalendarGoogleClientSecret.Set("client-secret")
	config.KeyvalueType.Set("redis")
	stop := errors.New("stop before Google")
	const userID = 7
	read := false

	err := NewService().ReconcileDeadline(t.Context(), userID, "account", 1, func() (DeadlineState, error) {
		read = true
		publicationLocks.Lock()
		entry := publicationLocks.byUser[userID]
		publicationLocks.Unlock()
		require.NotNil(t, entry)
		assert.False(t, entry.mutex.TryLock(), "the task is read inside the publication lock")
		return DeadlineState{}, stop
	})

	require.ErrorIs(t, err, stop)
	assert.True(t, read)
}
