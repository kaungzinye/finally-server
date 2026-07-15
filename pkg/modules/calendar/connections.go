// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package calendar

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/modules/keyvalue"
	"code.vikunja.io/api/pkg/utils"
)

var ErrDurableStoreRequired = errors.New("calendar connections require keyvalue.type=redis")
var ErrStableEncryptionKeyRequired = errors.New("calendar connections require calendar.encryptionkey")
var ErrAccountNotFound = errors.New("calendar account not found")
var ErrGoogleRequest = errors.New("google calendar request failed")

const maxGoogleEventPagesPerRequest = 10

type connectionUserLock struct {
	mutex sync.Mutex
	refs  int
}

var connectionLocks = struct {
	sync.Mutex
	byUser map[int64]*connectionUserLock
}{byUser: map[int64]*connectionUserLock{}}

type Account struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type storedAccount struct {
	Account
	Credentials string
}

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type googleUserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type EventAttendee struct {
	Email          string `json:"email"`
	ResponseStatus string `json:"response_status"`
}

type Event struct {
	AccountID   string          `json:"account_id"`
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Location    string          `json:"location,omitempty"`
	Start       string          `json:"start"`
	End         string          `json:"end"`
	Attendees   []EventAttendee `json:"attendees,omitempty"`
	Recurrence  []string        `json:"recurrence,omitempty"`
}

type googleEventsResponse struct {
	NextPageToken string `json:"nextPageToken"`
	Items         []struct {
		ID          string `json:"id"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Location    string `json:"location"`
		Start       struct {
			DateTime string `json:"dateTime"`
			Date     string `json:"date"`
		} `json:"start"`
		End struct {
			DateTime string `json:"dateTime"`
			Date     string `json:"date"`
		} `json:"end"`
		Attendees []struct {
			Email          string `json:"email"`
			ResponseStatus string `json:"responseStatus"`
		} `json:"attendees"`
		Recurrence []string `json:"recurrence"`
	} `json:"items"`
}

type Service struct {
	client       *http.Client
	clientID     string
	clientSecret string
	tokenURL     string
	apiURL       string
	revokeURL    string
	secret       string
}

func NewService() *Service {
	return &Service{
		client:       utils.NewSSRFSafeHTTPClient(),
		clientID:     config.CalendarGoogleClientID.GetString(),
		clientSecret: config.CalendarGoogleClientSecret.GetString(),
		tokenURL:     config.CalendarGoogleTokenURL.GetString(),
		apiURL:       strings.TrimRight(config.CalendarGoogleAPIURL.GetString(), "/"),
		revokeURL:    config.CalendarGoogleRevokeURL.GetString(),
		secret:       config.CalendarEncryptionKey.GetString(),
	}
}

func (s *Service) Connect(ctx context.Context, userID int64, code, redirectURI string) (*Account, error) {
	if err := s.validateStore(); err != nil {
		return nil, fmt.Errorf("validate calendar connection: %w", err)
	}
	form := url.Values{
		"code":          {code},
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	var token googleTokenResponse
	if err := s.postFormJSON(ctx, s.tokenURL, form, &token); err != nil {
		return nil, googleRequestError("exchange authorization code", err)
	}
	if token.AccessToken == "" || token.RefreshToken == "" {
		return nil, googleRequestError("token response is missing credentials", nil)
	}
	var profile googleUserInfo
	if err := s.getJSON(ctx, s.apiURL+"/oauth2/v2/userinfo", token.AccessToken, &profile); err != nil {
		return nil, googleRequestError("read account profile", err)
	}
	if profile.ID == "" || profile.Email == "" {
		return nil, googleRequestError("account profile is incomplete", nil)
	}

	sealed, err := s.seal(credentials{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second),
	})
	if err != nil {
		return nil, fmt.Errorf("seal calendar credentials: %w", err)
	}
	unlock := lockConnections(userID)
	defer unlock()
	accounts, err := s.load(userID)
	if err != nil {
		return nil, fmt.Errorf("connect calendar account: %w", err)
	}
	entry := storedAccount{Account: Account(profile), Credentials: sealed}
	found := false
	for i := range accounts {
		if accounts[i].ID == entry.ID {
			accounts[i] = entry
			found = true
			break
		}
	}
	if !found {
		accounts = append(accounts, entry)
	}
	if err := s.save(userID, accounts); err != nil {
		return nil, fmt.Errorf("connect calendar account: %w", err)
	}
	return &entry.Account, nil
}

func (s *Service) ReadContext(ctx context.Context, userID int64, accountIDs []string, from, to time.Time) ([]Event, error) {
	if err := s.validateStore(); err != nil {
		return nil, fmt.Errorf("validate calendar context request: %w", err)
	}
	events := []Event{}
	pagesRead := 0
	for _, accountID := range accountIDs {
		credentials, err := s.activeCredentials(ctx, userID, accountID)
		if err != nil {
			return nil, fmt.Errorf("load active credentials for calendar account %q: %w", accountID, err)
		}
		endpoint, err := url.Parse(s.apiURL + "/calendar/v3/calendars/primary/events")
		if err != nil {
			return nil, fmt.Errorf("parse Google Calendar events URL: %w", err)
		}
		pageToken := ""
		for {
			if pagesRead == maxGoogleEventPagesPerRequest {
				return nil, googleRequestError("read events exceeded the page limit", nil)
			}
			pagesRead++
			query := endpoint.Query()
			query.Set("timeMin", from.UTC().Format(time.RFC3339))
			query.Set("timeMax", to.UTC().Format(time.RFC3339))
			query.Set("singleEvents", "true")
			if pageToken != "" {
				query.Set("pageToken", pageToken)
			}
			endpoint.RawQuery = query.Encode()
			var response googleEventsResponse
			if err := s.getJSON(ctx, endpoint.String(), credentials.AccessToken, &response); err != nil {
				return nil, googleRequestError("read events", err)
			}
			for _, item := range response.Items {
				start := item.Start.DateTime
				if start == "" {
					start = item.Start.Date
				}
				end := item.End.DateTime
				if end == "" {
					end = item.End.Date
				}
				event := Event{AccountID: accountID, ID: item.ID, Title: item.Summary, Description: item.Description, Location: item.Location, Start: start, End: end, Recurrence: item.Recurrence}
				for _, attendee := range item.Attendees {
					event.Attendees = append(event.Attendees, EventAttendee{Email: attendee.Email, ResponseStatus: attendee.ResponseStatus})
				}
				events = append(events, event)
			}
			if response.NextPageToken == "" {
				break
			}
			pageToken = response.NextPageToken
		}
	}
	return events, nil
}

func (s *Service) activeCredentials(ctx context.Context, userID int64, accountID string) (credentials, error) {
	unlock := lockConnections(userID)
	defer unlock()
	accounts, err := s.load(userID)
	if err != nil {
		return credentials{}, fmt.Errorf("load active calendar credentials: %w", err)
	}
	for i, account := range accounts {
		if account.ID != accountID {
			continue
		}
		current, err := s.unseal(account.Credentials)
		if err != nil {
			return credentials{}, fmt.Errorf("open calendar credentials: %w", err)
		}
		if current.ExpiresAt.After(time.Now().UTC().Add(30 * time.Second)) {
			return current, nil
		}
		var refreshed googleTokenResponse
		if err := s.postFormJSON(ctx, s.tokenURL, url.Values{
			"client_id":     {s.clientID},
			"client_secret": {s.clientSecret},
			"refresh_token": {current.RefreshToken},
			"grant_type":    {"refresh_token"},
		}, &refreshed); err != nil {
			return credentials{}, googleRequestError("refresh credentials", err)
		}
		if refreshed.AccessToken == "" {
			return credentials{}, googleRequestError("refresh response is missing an access token", nil)
		}
		current.AccessToken = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			current.RefreshToken = refreshed.RefreshToken
		}
		current.ExpiresAt = time.Now().UTC().Add(time.Duration(refreshed.ExpiresIn) * time.Second)
		sealed, err := s.seal(current)
		if err != nil {
			return credentials{}, fmt.Errorf("seal refreshed calendar credentials: %w", err)
		}
		accounts[i].Credentials = sealed
		if err := s.save(userID, accounts); err != nil {
			return credentials{}, fmt.Errorf("persist refreshed calendar credentials: %w", err)
		}
		return current, nil
	}
	return credentials{}, ErrAccountNotFound
}

func (s *Service) Revoke(ctx context.Context, userID int64, accountID string) error {
	if err := s.validateStore(); err != nil {
		return fmt.Errorf("validate calendar revocation: %w", err)
	}
	token, err := func() (string, error) {
		unlock := lockConnections(userID)
		defer unlock()
		accounts, err := s.load(userID)
		if err != nil {
			return "", fmt.Errorf("load account for calendar revocation: %w", err)
		}
		for i, account := range accounts {
			if account.ID != accountID {
				continue
			}
			credentials, err := s.unseal(account.Credentials)
			if err != nil {
				return "", fmt.Errorf("open calendar credentials: %w", err)
			}
			token := credentials.RefreshToken
			if token == "" {
				token = credentials.AccessToken
			}
			accounts = append(accounts[:i], accounts[i+1:]...)
			if err := s.save(userID, accounts); err != nil {
				return "", fmt.Errorf("remove revoked calendar account: %w", err)
			}
			return token, nil
		}
		return "", ErrAccountNotFound
	}()
	if err != nil {
		return fmt.Errorf("revoke calendar account: %w", err)
	}
	if err := s.postForm(ctx, s.revokeURL, url.Values{"token": {token}}); err != nil {
		return googleRequestError("revoke credentials", err)
	}
	return nil
}

func (s *Service) List(userID int64) ([]Account, error) {
	if err := s.validateStore(); err != nil {
		return nil, fmt.Errorf("validate calendar account listing: %w", err)
	}
	unlock := lockConnections(userID)
	defer unlock()
	stored, err := s.load(userID)
	if err != nil {
		return nil, fmt.Errorf("list calendar accounts: %w", err)
	}
	accounts := make([]Account, 0, len(stored))
	for _, entry := range stored {
		accounts = append(accounts, entry.Account)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	return accounts, nil
}

func lockConnections(userID int64) func() {
	connectionLocks.Lock()
	entry := connectionLocks.byUser[userID]
	if entry == nil {
		entry = &connectionUserLock{}
		connectionLocks.byUser[userID] = entry
	}
	entry.refs++
	connectionLocks.Unlock()

	entry.mutex.Lock()
	return func() {
		entry.mutex.Unlock()
		connectionLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(connectionLocks.byUser, userID)
		}
		connectionLocks.Unlock()
	}
}

func googleRequestError(action string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrGoogleRequest, action)
	}
	return fmt.Errorf("%w: %s: %w", ErrGoogleRequest, action, err)
}

func (s *Service) validateStore() error {
	if config.KeyvalueType.GetString() != "redis" {
		return ErrDurableStoreRequired
	}
	if s.secret == "" {
		return ErrStableEncryptionKeyRequired
	}
	if s.clientID == "" || s.clientSecret == "" {
		return errors.New("google calendar connection is not configured")
	}
	return nil
}

func (s *Service) load(userID int64) ([]storedAccount, error) {
	var accounts []storedAccount
	exists, err := keyvalue.GetWithValue(accountKey(userID), &accounts)
	if err != nil {
		return nil, fmt.Errorf("load calendar connections: %w", err)
	}
	if !exists {
		return []storedAccount{}, nil
	}
	return accounts, nil
}

func (s *Service) save(userID int64, accounts []storedAccount) error {
	if err := keyvalue.Put(accountKey(userID), accounts); err != nil {
		return fmt.Errorf("save calendar connections: %w", err)
	}
	return nil
}

func accountKey(userID int64) string {
	return "finally_calendar_accounts_" + strconv.FormatInt(userID, 10)
}

func (s *Service) seal(value credentials) (string, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode calendar credentials: %w", err)
	}
	key := s.encryptionKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("create calendar credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create calendar credential AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate calendar credential nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plain, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func (s *Service) unseal(value string) (credentials, error) {
	var result credentials
	ciphertext, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return result, fmt.Errorf("decode calendar credentials: %w", err)
	}
	key := s.encryptionKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return result, fmt.Errorf("create calendar credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return result, fmt.Errorf("create calendar credential AEAD: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return result, errors.New("encrypted calendar credentials are invalid")
	}
	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return result, fmt.Errorf("decrypt calendar credentials: %w", err)
	}
	if err := json.Unmarshal(plain, &result); err != nil {
		return result, fmt.Errorf("decode decrypted calendar credentials: %w", err)
	}
	return result, nil
}

func (s *Service) encryptionKey() [sha256.Size]byte {
	return sha256.Sum256([]byte("finally-calendar-credentials\x00" + s.secret))
}

func (s *Service) postFormJSON(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := newGoogleFormRequest(ctx, endpoint, form)
	if err != nil {
		return fmt.Errorf("prepare Google form request: %w", err)
	}
	if err := s.doJSON(req, out); err != nil {
		return fmt.Errorf("send Google form request: %w", err)
	}
	return nil
}

func (s *Service) postForm(ctx context.Context, endpoint string, form url.Values) error {
	req, err := newGoogleFormRequest(ctx, endpoint, form)
	if err != nil {
		return fmt.Errorf("prepare Google form request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Google form request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("google returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func newGoogleFormRequest(ctx context.Context, endpoint string, form url.Values) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create Google form request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func (s *Service) getJSON(ctx context.Context, endpoint, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Google API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if err := s.doJSON(req, out); err != nil {
		return fmt.Errorf("send Google API request: %w", err)
	}
	return nil
}

func (s *Service) doJSON(req *http.Request, out any) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Google request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("google returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode Google response: %w", err)
	}
	return nil
}
