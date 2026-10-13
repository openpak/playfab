package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeIdentity struct {
	mu                   sync.Mutex
	status               int
	p                    identity
	tokenCalls, pidCalls int
}

func testService(t *testing.T) (*server, *fakeIdentity, string) {
	t.Helper()
	return testServiceWithApp(t, coreKeeperApplication)
}

func testServiceWithApp(t *testing.T, app string) (*server, *fakeIdentity, string) {
	t.Helper()
	fake := &fakeIdentity{status: 200, p: identity{PID: 1800000004, AccountID: "acc-4", BaasUserID: "aaaa", Nickname: "Keeper", ApplicationID: app}}
	nx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if r.URL.Path != "/internal/switch/identity" || r.Method != "POST" || r.Header.Get("X-Internal-Key") != "internal-key" {
			t.Errorf("incorrect identity call")
			w.WriteHeader(401)
			return
		}
		var in map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in) != 1 {
			t.Errorf("identity input must contain only proven token or PID")
			w.WriteHeader(400)
			return
		}
		if b, ok := in["id_token"]; ok {
			fake.tokenCalls++
			var token string
			_ = json.Unmarshal(b, &token)
			if token != "proven-nintendo-token" {
				w.WriteHeader(401)
				return
			}
		} else if b, ok := in["pid"]; ok {
			fake.pidCalls++
			var pid uint64
			_ = json.Unmarshal(b, &pid)
			if pid != 1800000004 {
				w.WriteHeader(404)
				return
			}
		} else {
			t.Errorf("no identity proof")
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(fake.status)
		if fake.status == 200 {
			_ = json.NewEncoder(w).Encode(fake.p)
		}
	}))
	t.Cleanup(nx.Close)
	dir := t.TempDir()
	state, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{state: state, ids: newIdentityClient(nx.URL, "internal-key"), now: func() time.Time { return time.Date(2026, 10, 12, 19, 0, 0, 0, time.UTC) }, titles: defaultTitles()}
	t.Cleanup(func() { _ = s.state.close() })
	return s, fake, dir
}

func callHost(s *server, host, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://"+host+path, strings.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func call(s *server, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	return callHost(s, coreKeeperHost, path, body, headers)
}
func loginBody(create bool) string {
	return fmt.Sprintf(`{"TitleId":"94845","IdentityToken":"proven-nintendo-token","CreateAccount":%t}`, create)
}
func loggedIn(t *testing.T, s *server) loginResult {
	t.Helper()
	w := call(s, "/Client/LoginWithNintendoServiceAccount", loginBody(true), nil)
	return loginData(t, w)
}
func loginData(t *testing.T, w *httptest.ResponseRecorder) loginResult {
	t.Helper()
	var v struct {
		Code   int         `json:"code"`
		Status string      `json:"status"`
		Data   loginResult `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Code != 200 || v.Status != "OK" {
		t.Fatalf("login failed: %d %s", w.Code, w.Body)
	}
	return v.Data
}
func entityData(t *testing.T, w *httptest.ResponseRecorder) entityResult {
	t.Helper()
	var v struct {
		Code   int          `json:"code"`
		Status string       `json:"status"`
		Data   entityResult `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Code != 200 || v.Status != "OK" {
		t.Fatalf("exchange failed: %d %s", w.Code, w.Body)
	}
	return v.Data
}

func TestLoginPersistsIdentityAndSigningMaterial(t *testing.T) {
	s, fake, dir := testService(t)
	w := call(s, "/Client/LoginWithNintendoServiceAccount", loginBody(false), nil)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "AccountNotFound") {
		t.Fatalf("CreateAccount=false created a player: %s", w.Body)
	}
	first := loggedIn(t, s)
	if !first.NewlyCreated || first.PlayFabID == "" || first.SessionTicket == "" || first.EntityToken.Entity.ID == "" || first.EntityToken.Entity.Type != "title_player_account" || first.EntityToken.EntityToken == "" {
		t.Fatalf("incomplete SDK login: %+v", first)
	}
	if first.SessionTicket == first.EntityToken.EntityToken || !first.EntityToken.TokenExpiration.Equal(s.now().Add(time.Hour)) {
		t.Fatal("incorrect credentials or expiry")
	}
	if !first.LastLoginTime.Equal(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("first login has a fabricated previous time")
	}
	_ = s.state.close()
	var err error
	s.state, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	next := loginData(t, call(s, "/Client/LoginWithNintendoServiceAccount", loginBody(false), nil))
	if next.NewlyCreated || next.PlayFabID != first.PlayFabID || next.EntityToken.Entity != first.EntityToken.Entity || !next.LastLoginTime.Equal(s.now()) {
		t.Fatal("player changed after restart")
	}
	if next.SessionTicket == first.SessionTicket || next.EntityToken.EntityToken == first.EntityToken.EntityToken {
		t.Fatal("shared credentials")
	}
	for _, h := range []map[string]string{{"X-Authentication": first.SessionTicket}, {"X-EntityToken": first.EntityToken.EntityToken}} {
		exchanged := entityData(t, call(s, "/Authentication/GetEntityToken", "{}", h))
		if exchanged.Entity != first.EntityToken.Entity {
			t.Fatal("entity changed on exchange")
		}
	}
	if fake.pidCalls != 2 || fake.tokenCalls != 3 {
		t.Fatal("identity not checked on login and refresh")
	}
}

func TestLoginRequiresVerifiedAccountAndApplication(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		change func(*identity)
		want   int
	}{
		{"banned", 403, nil, 403}, {"deleted", 410, nil, 403}, {"unknown", 404, nil, 401}, {"unavailable", 503, nil, 503},
		{"wrong application", 200, func(p *identity) { p.ApplicationID = "0100000000000000" }, 400},
		{"missing application", 200, func(p *identity) { p.ApplicationID = "" }, 400},
		{"unlinked", 200, func(p *identity) { p.AccountID = "" }, 401},
		{"missing PID", 200, func(p *identity) { p.PID = 0 }, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, fake, _ := testService(t)
			fake.status = test.status
			if test.change != nil {
				test.change(&fake.p)
			}
			w := call(s, "/Client/LoginWithNintendoServiceAccount", loginBody(true), nil)
			if w.Code != test.want {
				t.Fatalf("status %d want %d: %s", w.Code, test.want, w.Body)
			}
			if len(s.state.Players) != 0 {
				t.Fatal("rejected login created a mapping")
			}
		})
	}
	s, fake, _ := testService(t)
	for _, body := range []string{
		`{"TitleId":"00000","IdentityToken":"proven-nintendo-token","CreateAccount":true}`,
		`{"TitleId":"94845","pid":1800000004,"CreateAccount":true}`,
		`{"TitleId":"94845","IdentityToken":"unsigned-jwt","CreateAccount":true}`,
	} {
		if w := call(s, "/Client/LoginWithNintendoServiceAccount", body, nil); w.Code == 200 {
			t.Fatal("unproven or wrong-title login accepted")
		}
	}
	if fake.tokenCalls != 1 {
		t.Fatal("invalid input reached identity lookup")
	}
}

func TestExchangeRejectsWrongExpiredOrTamperedCredentials(t *testing.T) {
	s, _, _ := testService(t)
	login := loggedIn(t, s)
	p, _ := s.state.player(coreKeeperTitle, "acc-4")
	id := identity{PID: 1800000004, BaasUserID: "aaaa"}
	wrong := p
	wrong.TitleID = "00000"
	wrongTitle, _, _ := s.issue(wrong, id, sessionAudience, s.now())
	expired, _, _ := s.issue(p, id, sessionAudience, s.now().Add(-time.Hour))
	future, _, _ := s.issue(p, id, sessionAudience, s.now().Add(time.Minute))
	otherEntity := p
	otherEntity.EntityID = "different"
	wrongEntity, _, _ := s.issue(otherEntity, id, sessionAudience, s.now())
	for name, h := range map[string]map[string]string{
		"missing":          nil,
		"junk":             {"X-Authentication": "junk"},
		"tampered":         {"X-Authentication": login.SessionTicket + "x"},
		"wrong title":      {"X-Authentication": wrongTitle},
		"expired":          {"X-Authentication": expired},
		"future":           {"X-Authentication": future},
		"wrong entity":     {"X-Authentication": wrongEntity},
		"entity as ticket": {"X-Authentication": login.EntityToken.EntityToken},
		"ticket as entity": {"X-EntityToken": login.SessionTicket},
		"ambiguous":        {"X-Authentication": login.SessionTicket, "X-EntityToken": login.EntityToken.EntityToken},
		"secret":           {"X-Authentication": login.SessionTicket, "X-SecretKey": "no"},
	} {
		t.Run(name, func(t *testing.T) {
			if w := call(s, "/Authentication/GetEntityToken", "{}", h); w.Code != 401 {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
	if w := call(s, "/Authentication/GetEntityToken", `{"Entity":{"Id":"other","Type":"title_player_account"}}`, map[string]string{"X-Authentication": login.SessionTicket}); w.Code != 403 {
		t.Fatal("entity escalation")
	}
	before := s.now()
	s.now = func() time.Time { return before.Add(30 * time.Minute) }
	refreshed := entityData(t, call(s, "/Authentication/GetEntityToken", "{}", map[string]string{"X-EntityToken": login.EntityToken.EntityToken}))
	if refreshed.Entity != login.EntityToken.Entity || !refreshed.TokenExpiration.Equal(s.now().Add(time.Hour)) {
		t.Fatal("refresh did not renew expiry")
	}
}

func TestRefreshPreservesLocksAndLinkage(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		change func(*identity)
		want   int
	}{
		{"ban", 403, nil, 403}, {"deleted", 410, nil, 403}, {"unavailable", 503, nil, 503},
		{"relinked", 200, func(p *identity) { p.AccountID = "other" }, 401},
		{"new Nintendo account", 200, func(p *identity) { p.BaasUserID = "other" }, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, fake, _ := testService(t)
			login := loggedIn(t, s)
			fake.status = test.status
			if test.change != nil {
				test.change(&fake.p)
			}
			if w := call(s, "/Authentication/GetEntityToken", "{}", map[string]string{"X-Authentication": login.SessionTicket}); w.Code != test.want {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
}

func TestHostBoundsJSONAndCredentialLogging(t *testing.T) {
	s, _, _ := testService(t)
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(old) })
	for _, body := range []string{"null", "[]", "{} {}", `{"IdentityToken":`, `{"TitleId":94845}`} {
		if w := call(s, "/Client/LoginWithNintendoServiceAccount", body, nil); w.Code != 400 {
			t.Fatalf("bad JSON: %d", w.Code)
		}
	}
	if w := call(s, "/Client/LoginWithNintendoServiceAccount", strings.Repeat(" ", maxBody+1), nil); w.Code != 413 {
		t.Fatal("unbounded body")
	}
	for _, host := range []string{"94845.playfabapi.com.evil", "00000.playfabapi.com", "playfabapi.com"} {
		r := httptest.NewRequest("POST", "http://"+host+"/Client/LoginWithNintendoServiceAccount", strings.NewReader(loginBody(true)))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 503 {
			t.Fatal("wrong host accepted")
		}
	}
	if w := call(s, "/MultiplayerServer/ListPartyQosServers", "{}", nil); w.Code != 503 || !strings.Contains(w.Body.String(), "ServiceUnavailable") {
		t.Fatal("fake QoS response")
	}
	_ = loggedIn(t, s)
	_ = call(s, "/Client/LoginWithNintendoServiceAccount?IdentityToken=query-secret", strings.ReplaceAll(loginBody(true), "proven-nintendo-token", "body-secret"), nil)
	_ = call(s, "/Authentication/GetEntityToken", "{}", map[string]string{"X-Authentication": "header-secret"})
	_ = call(s, "/Client/path-secret-123", "body-secret", nil)
	for _, secret := range []string{"proven-nintendo-token", "query-secret", "body-secret", "header-secret", "path-secret-123"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("credential logged: %s", secret)
		}
	}
}

func TestCombinedInfoContainsOnlyLocalData(t *testing.T) {
	s, _, _ := testService(t)
	_ = loggedIn(t, s)
	body := strings.TrimSuffix(loginBody(false), "}") + `,"InfoRequestParameters":{"GetUserAccountInfo":true,"GetUserInventory":true,"GetUserData":true,"GetUserVirtualCurrency":true,"GetPlayerProfile":true}}`
	result := loginData(t, call(s, "/Client/LoginWithNintendoServiceAccount", body, nil))
	payload := result.InfoResultPayload
	if len(payload["UserInventory"].([]any)) != 0 || len(payload["UserData"].(map[string]any)) != 0 || len(payload["UserVirtualCurrency"].(map[string]any)) != 0 {
		t.Fatal("fabricated game data")
	}
	if payload["AccountInfo"].(map[string]any)["PlayFabId"] != result.PlayFabID || payload["PlayerProfile"].(map[string]any)["DisplayName"] != "Keeper" {
		t.Fatal("missing requested local identity")
	}
}

func TestConcurrentCreateHasOneNewPlayer(t *testing.T) {
	s, _, _ := testService(t)
	var wg sync.WaitGroup
	results := make(chan loginResult, 12)
	for range 12 {
		wg.Go(func() {
			w := call(s, "/Client/LoginWithNintendoServiceAccount", loginBody(true), nil)
			results <- loginData(t, w)
		})
	}
	wg.Wait()
	close(results)
	count := 0
	id := ""
	for r := range results {
		if r.NewlyCreated {
			count++
		}
		if id != "" && id != r.PlayFabID {
			t.Fatal("duplicate identity")
		}
		id = r.PlayFabID
	}
	if count != 1 {
		t.Fatalf("%d newly created players", count)
	}
}

func TestIdentityUnavailableDoesNotFollowRedirects(t *testing.T) {
	s, _, _ := testService(t)
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; _, _ = io.Copy(io.Discard, r.Body) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	s.ids = newIdentityClient(redirect.URL, "internal-key")
	if w := call(s, "/Client/LoginWithNintendoServiceAccount", loginBody(true), nil); w.Code != 503 || called {
		t.Fatal("internal credential followed redirect")
	}
}

func TestDoomTitleIsIsolatedFromCoreKeeper(t *testing.T) {
	t.Setenv("PLAYFAB_DOOM_TITLE", "DOOM1")
	s, fake, _ := testServiceWithApp(t, coreKeeperApplication)
	doomHost := "doom1.playfabapi.com"
	if _, ok := s.titles[doomHost]; !ok {
		t.Fatalf("doom host %s not served", doomHost)
	}
	// Same console and account on both titles: the Nintendo token carries a
	// different application per title.
	keeper := loggedIn(t, s)
	fake.mu.Lock()
	fake.p.ApplicationID = doomApplication
	fake.mu.Unlock()
	doomBody := `{"TitleId":"DOOM1","IdentityToken":"proven-nintendo-token","CreateAccount":true}`
	doom := loginData(t, callHost(s, doomHost, "/Client/LoginWithNintendoServiceAccount", doomBody, nil))
	if doom.PlayFabID == keeper.PlayFabID || doom.EntityToken.Entity.ID == keeper.EntityToken.Entity.ID {
		t.Fatal("doom login reused the Core Keeper identity")
	}
	if doom.SessionTicket == keeper.SessionTicket {
		t.Fatal("cross-title credential")
	}
	// A Core Keeper ticket is not valid on the DOOM host, and vice versa.
	if w := callHost(s, doomHost, "/Authentication/GetEntityToken", "{}", map[string]string{"X-Authentication": keeper.SessionTicket}); w.Code != 401 {
		t.Fatalf("keeper ticket accepted on doom host: %d", w.Code)
	}
	if w := call(s, "/Authentication/GetEntityToken", "{}", map[string]string{"X-Authentication": doom.SessionTicket}); w.Code != 401 {
		t.Fatalf("doom ticket accepted on keeper host: %d", w.Code)
	}
	// Each title exchanges its own credential.
	if got := entityData(t, callHost(s, doomHost, "/Authentication/GetEntityToken", "{}", map[string]string{"X-Authentication": doom.SessionTicket})); got.Entity != doom.EntityToken.Entity {
		t.Fatal("doom entity changed on exchange")
	}
}

func TestDoomStaysOffWithoutTitleId(t *testing.T) {
	s, _, _ := testService(t)
	if len(s.titles) != 1 {
		t.Fatalf("%d titles, want 1", len(s.titles))
	}
	r := httptest.NewRequest("POST", "https://doom1.playfabapi.com/Client/LoginWithNintendoServiceAccount", strings.NewReader(`{"TitleId":"DOOM1","IdentityToken":"proven-nintendo-token","CreateAccount":true}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("unconfigured doom host answered %d", w.Code)
	}
}
