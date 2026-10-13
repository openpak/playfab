package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// nx-baas owns verification of Nintendo credentials and account locks. Refresh
// uses only the PID from our verified credential, and checks the linked account
// still matches before issuing anything.
type identity struct {
	PID           uint64 `json:"pid"`
	AccountID     string `json:"account_id"`
	BaasUserID    string `json:"baas_user_id"`
	Nickname      string `json:"nickname"`
	ApplicationID string `json:"application_id"`
}

var (
	errIdentity    = errors.New("identity rejected")
	errLocked      = errors.New("account locked")
	errUnavailable = errors.New("identity unavailable")
)

type identityClient struct {
	url, key string
	client   *http.Client
}

func newIdentityClient(base, key string) identityClient {
	return identityClient{strings.TrimRight(base, "/"), key, &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c identityClient) lookup(ctx context.Context, input any) (identity, error) {
	var p identity
	body, err := json.Marshal(input)
	if err != nil {
		return p, errUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/internal/switch/identity", bytes.NewReader(body))
	if err != nil {
		return p, errUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", c.key)
	resp, err := c.client.Do(req)
	if err != nil {
		return p, errUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusNotFound:
		return p, errIdentity
	case http.StatusForbidden, http.StatusGone:
		return p, errLocked
	default:
		return p, errUnavailable
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&p) != nil {
		return p, errUnavailable
	}
	if p.PID == 0 || p.AccountID == "" || p.BaasUserID == "" {
		return p, errIdentity
	}
	return p, nil
}
