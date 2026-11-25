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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const deadlineCalendarName = "Deadlines"

type googleCalendarList struct {
	Items []googleCalendar `json:"items"`
}

type googleCalendar struct {
	ID      string `json:"id,omitempty"`
	Summary string `json:"summary"`
}

type deadlineEventBoundary struct {
	Date     string `json:"date,omitempty"`
	DateTime string `json:"dateTime,omitempty"`
}

// The phone is the only thing that notifies, so markers opt out of the calendar's default reminders.
type deadlineReminders struct {
	UseDefault bool `json:"useDefault"`
}

type deadlineEvent struct {
	ID           string                `json:"id,omitempty"`
	Status       string                `json:"status"`
	Summary      string                `json:"summary"`
	Reminders    deadlineReminders     `json:"reminders"`
	Transparency string                `json:"transparency"`
	Start        deadlineEventBoundary `json:"start"`
	End          deadlineEventBoundary `json:"end"`
}

// ReconcileDeadline makes one task's marker match its authoritative deadline state.
func (s *Service) ReconcileDeadline(ctx context.Context, userID int64, accountID string, taskID int64, title string, deadline time.Time, hasTime, active bool) error {
	if err := s.validateStore(); err != nil {
		return fmt.Errorf("validate deadline reconciliation: %w", err)
	}
	unlock := lockConnections(userID)
	defer unlock()
	credentials, err := s.activeCredentialsLocked(ctx, userID, accountID)
	if err != nil {
		return fmt.Errorf("load deadline calendar credentials: %w", err)
	}
	calendarID, err := s.deadlineCalendar(ctx, credentials.AccessToken, active)
	if err != nil {
		return err
	}
	if calendarID == "" {
		return nil
	}
	eventID := deadlineEventID(userID, taskID)
	eventsEndpoint := s.apiURL + "/calendar/v3/calendars/" + url.PathEscape(calendarID) + "/events"
	endpoint := eventsEndpoint + "/" + eventID
	if !active {
		if err := s.deleteGoogleResource(ctx, endpoint, credentials.AccessToken); err != nil {
			return googleRequestError("delete deadline marker", err)
		}
		return nil
	}

	// Google keeps a deleted event ID as cancelled. Confirmed status restores it when a task reopens.
	event := deadlineEvent{ID: eventID, Status: "confirmed", Summary: title, Transparency: "transparent"}
	if hasTime {
		event.Start.DateTime = deadline.UTC().Format(time.RFC3339)
		event.End.DateTime = deadline.UTC().Add(time.Minute).Format(time.RFC3339)
	} else {
		event.Start.Date = deadline.Format(time.DateOnly)
		event.End.Date = deadline.AddDate(0, 0, 1).Format(time.DateOnly)
	}
	if err := s.upsertDeadlineEvent(ctx, eventsEndpoint, endpoint, credentials.AccessToken, event); err != nil {
		return googleRequestError("upsert deadline marker", err)
	}
	return nil
}

func (s *Service) deadlineCalendar(ctx context.Context, accessToken string, create bool) (string, error) {
	var calendars googleCalendarList
	if err := s.getJSON(ctx, s.apiURL+"/calendar/v3/users/me/calendarList", accessToken, &calendars); err != nil {
		return "", googleRequestError("list calendars for deadline marker", err)
	}
	for _, calendar := range calendars.Items {
		if calendar.Summary == deadlineCalendarName {
			return calendar.ID, nil
		}
	}
	if !create {
		return "", nil
	}
	var created googleCalendar
	if err := s.sendGoogleJSON(ctx, http.MethodPost, s.apiURL+"/calendar/v3/calendars", accessToken, googleCalendar{Summary: deadlineCalendarName}, &created); err != nil {
		return "", googleRequestError("create deadline calendar", err)
	}
	if created.ID == "" {
		return "", googleRequestError("created deadline calendar is missing an id", nil)
	}
	return created.ID, nil
}

func deadlineEventID(userID, taskID int64) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(taskID, 10)))
	return "f1" + hex.EncodeToString(sum[:16])
}

func (s *Service) upsertDeadlineEvent(ctx context.Context, collectionEndpoint, itemEndpoint, accessToken string, event deadlineEvent) error {
	status, err := s.sendGoogleJSONStatus(ctx, http.MethodPost, collectionEndpoint, accessToken, event, &struct{}{})
	if err == nil {
		return nil
	}
	if status != http.StatusConflict {
		return err
	}
	event.ID = ""
	return s.sendGoogleJSON(ctx, http.MethodPut, itemEndpoint, accessToken, event, &struct{}{})
}

func (s *Service) sendGoogleJSON(ctx context.Context, method, endpoint, accessToken string, body, out any) error {
	_, err := s.sendGoogleJSONStatus(ctx, method, endpoint, accessToken, body, out)
	return err
}

func (s *Service) sendGoogleJSONStatus(ctx context.Context, method, endpoint, accessToken string, body, out any) (int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("encode Google request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return 0, fmt.Errorf("create Google API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send Google API request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, fmt.Errorf("google returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode Google response: %w", err)
	}
	return resp.StatusCode, nil
}

func (s *Service) deleteGoogleResource(ctx context.Context, endpoint, accessToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Google API delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Google API delete request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf("google returned HTTP %d", resp.StatusCode)
}
