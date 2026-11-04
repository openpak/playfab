package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	acctA = "11111111-1111-4111-8111-111111111111"
	acctB = "22222222-2222-4222-8222-222222222222"
)

// Against a real Postgres (PLAYFAB_TEST_DATABASE_URL): erase drops every title row of one account.
func TestEraseRemovesOnlyThatAccount(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	s, err := openStore(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	now := time.Now()
	for _, c := range [][2]string{{"94845", acctA}, {"other", acctA}, {"94845", acctB}} {
		if _, _, err := s.login(c[0], c[1], true, now); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.erase(ctx, acctA)
	if err != nil || n != 2 {
		t.Fatalf("erase: %d %v", n, err)
	}
	if _, ok := s.player("94845", acctA); ok {
		t.Fatal("row kept")
	}
	if _, ok := s.player("94845", acctB); !ok {
		t.Fatal("other account erased")
	}
	if n, err := s.erase(ctx, acctA); err != nil || n != 0 {
		t.Fatalf("second erase: %d %v", n, err)
	}
}

func TestEraseEndpoint(t *testing.T) {
	h := eraseHandler("playfab", func(context.Context, string) (int, error) { return 2, nil })
	call := func(id, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("DELETE", "/internal/accounts/"+id+"/data", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	t.Setenv("ACCOUNT_ERASE_KEY", "")
	if r := call(acctA, "Bearer k"); r.Code != 503 {
		t.Fatalf("unset key: %d", r.Code)
	}
	t.Setenv("ACCOUNT_ERASE_KEY", "k")
	if r := call(acctA, ""); r.Code != 401 {
		t.Fatalf("no key: %d", r.Code)
	}
	if r := call(acctA, "Bearer bad"); r.Code != 401 {
		t.Fatalf("wrong key: %d", r.Code)
	}
	if r := call("nope", "Bearer k"); r.Code != 400 {
		t.Fatalf("bad id: %d", r.Code)
	}
	if r := call(acctA, "Bearer k"); r.Code != 200 || !strings.Contains(r.Body.String(), `"service":"playfab","rows":2`) {
		t.Fatalf("ok: %d %s", r.Code, r.Body.String())
	}
}
