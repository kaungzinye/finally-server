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
	"errors"
	"net/http"
	"time"

	calendarcontext "code.vikunja.io/api/pkg/modules/calendar"
	"code.vikunja.io/api/pkg/user"

	"github.com/danielgtaylor/huma/v2"
)

type finallyCalendarConnectInput struct {
	Body struct {
		AuthorizationCode string `json:"authorization_code" valid:"required" maxLength:"8192" doc:"The one-time Google OAuth authorization code."`
		RedirectURI       string `json:"redirect_uri" valid:"required" maxLength:"2048" doc:"The redirect URI used to obtain the authorization code."`
	}
}

type finallyCalendarAccountBody struct {
	Body *calendarcontext.Account
}

type finallyCalendarAccountsBody struct {
	Body []calendarcontext.Account
}

type finallyCalendarContextInput struct {
	Body struct {
		AccountIDs []string  `json:"account_ids" doc:"The connected accounts to read."`
		From       time.Time `json:"from" doc:"The inclusive start of the planning window."`
		To         time.Time `json:"to" doc:"The exclusive end of the planning window."`
	}
}

type finallyCalendarContextBody struct {
	Body []calendarcontext.Event
}

const (
	maxFinallyCalendarPlanningWindow = 31 * 24 * time.Hour
	maxFinallyCalendarAccounts       = 10
	maxFinallyCalendarRequestBody    = 16 << 10
)

func RegisterFinallyCalendarRoutes(api huma.API) {
	tags := []string{"finally", "calendar"}
	Register(api, huma.Operation{
		OperationID:  "finally-calendar-accounts-connect",
		Summary:      "Connect a Google Calendar account",
		Method:       http.MethodPost,
		Path:         "/finally/calendar/accounts",
		MaxBodyBytes: maxFinallyCalendarRequestBody,
		Tags:         tags,
	}, finallyCalendarAccountsConnect)
	Register(api, huma.Operation{
		OperationID:   "finally-calendar-accounts-list",
		Summary:       "List connected Google Calendar accounts",
		Method:        http.MethodGet,
		Path:          "/finally/calendar/accounts",
		DefaultStatus: http.StatusOK,
		Tags:          tags,
	}, finallyCalendarAccountsList)
	Register(api, huma.Operation{
		OperationID: "finally-calendar-accounts-revoke",
		Summary:     "Revoke a Google Calendar account",
		Method:      http.MethodDelete,
		Path:        "/finally/calendar/accounts/{account}",
		Tags:        tags,
	}, finallyCalendarAccountsRevoke)
	Register(api, huma.Operation{
		OperationID:   "finally-calendar-context-read",
		Summary:       "Read calendar context for planning",
		Description:   "Fetches full event context from Google on demand. Event content is not retained by Finally Server.",
		Method:        http.MethodPost,
		Path:          "/finally/calendar/context",
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  maxFinallyCalendarRequestBody,
		Tags:          tags,
	}, finallyCalendarContextRead)
}

func finallyCalendarAccountsRevoke(ctx context.Context, in *struct {
	Account string `path:"account" doc:"The Google account id to revoke."`
}) (*emptyBody, error) {
	u, err := finallyCalendarUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := calendarcontext.NewService().Revoke(ctx, u.ID, in.Account); err != nil {
		return nil, finallyCalendarError(err)
	}
	return &emptyBody{}, nil
}

func finallyCalendarContextRead(ctx context.Context, in *finallyCalendarContextInput) (*finallyCalendarContextBody, error) {
	u, err := finallyCalendarUser(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.Body.AccountIDs) == 0 || in.Body.From.IsZero() || in.Body.To.IsZero() || !in.Body.From.Before(in.Body.To) {
		return nil, huma.Error400BadRequest("account_ids and a valid planning window are required")
	}
	if in.Body.To.Sub(in.Body.From) > maxFinallyCalendarPlanningWindow {
		return nil, huma.Error400BadRequest("planning window cannot exceed 31 days")
	}
	if len(in.Body.AccountIDs) > maxFinallyCalendarAccounts {
		return nil, huma.Error400BadRequest("planning context cannot include more than 10 accounts")
	}
	seenAccounts := make(map[string]struct{}, len(in.Body.AccountIDs))
	for _, accountID := range in.Body.AccountIDs {
		if _, exists := seenAccounts[accountID]; exists {
			return nil, huma.Error400BadRequest("account_ids cannot contain a duplicate account")
		}
		seenAccounts[accountID] = struct{}{}
	}
	events, err := calendarcontext.NewService().ReadContext(ctx, u.ID, in.Body.AccountIDs, in.Body.From, in.Body.To)
	if err != nil {
		return nil, finallyCalendarError(err)
	}
	return &finallyCalendarContextBody{Body: events}, nil
}

func finallyCalendarAccountsConnect(ctx context.Context, in *finallyCalendarConnectInput) (*finallyCalendarAccountBody, error) {
	u, err := finallyCalendarUser(ctx)
	if err != nil {
		return nil, err
	}
	account, err := calendarcontext.NewService().Connect(ctx, u.ID, in.Body.AuthorizationCode, in.Body.RedirectURI)
	if err != nil {
		return nil, finallyCalendarError(err)
	}
	return &finallyCalendarAccountBody{Body: account}, nil
}

func finallyCalendarAccountsList(ctx context.Context, _ *struct{}) (*finallyCalendarAccountsBody, error) {
	u, err := finallyCalendarUser(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := calendarcontext.NewService().List(u.ID)
	if err != nil {
		return nil, finallyCalendarError(err)
	}
	return &finallyCalendarAccountsBody{Body: accounts}, nil
}

func finallyCalendarUser(ctx context.Context) (*user.User, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	u, err := user.GetFromAuth(a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	return u, nil
}

func finallyCalendarError(err error) error {
	if errors.Is(err, calendarcontext.ErrDurableStoreRequired) || errors.Is(err, calendarcontext.ErrStableEncryptionKeyRequired) {
		return huma.Error503ServiceUnavailable(err.Error())
	}
	if errors.Is(err, calendarcontext.ErrAccountNotFound) {
		return huma.Error404NotFound(err.Error())
	}
	if errors.Is(err, calendarcontext.ErrGoogleRequest) {
		return huma.Error502BadGateway("Google Calendar request failed")
	}
	return huma.Error500InternalServerError("Calendar service failed")
}
