package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxBody = 32 << 10

type server struct {
	state  *store
	ids    identityClient
	now    func() time.Time
	qos    *partyQos
	titles map[string]titleConfig // lowercase host -> title; defaultTitles()
}

type entityKey struct {
	ID   string `json:"Id"`
	Type string `json:"Type"`
}
type entityResult struct {
	Entity          entityKey `json:"Entity"`
	EntityToken     string    `json:"EntityToken"`
	TokenExpiration time.Time `json:"TokenExpiration"`
}
type loginRequest struct {
	TitleID               string      `json:"TitleId"`
	IdentityToken         string      `json:"IdentityToken"`
	CreateAccount         bool        `json:"CreateAccount"`
	InfoRequestParameters *infoParams `json:"InfoRequestParameters"`
	EncryptedRequest      string      `json:"EncryptedRequest"`
	PlayerSecret          string      `json:"PlayerSecret"`
}
type loginResult struct {
	PlayFabID         string          `json:"PlayFabId"`
	SessionTicket     string          `json:"SessionTicket"`
	EntityToken       entityResult    `json:"EntityToken"`
	NewlyCreated      bool            `json:"NewlyCreated"`
	LastLoginTime     time.Time       `json:"LastLoginTime"`
	InfoResultPayload map[string]any  `json:"InfoResultPayload,omitempty"`
	SettingsForUser   map[string]bool `json:"SettingsForUser"`
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	body, err := json.Marshal(data)
	if err != nil {
		code = http.StatusInternalServerError
		body = []byte(`{"code":500,"status":"InternalServerError","error":"InternalServerError","errorCode":1110,"errorMessage":"Response encoding failed."}`)
	}
	body = append(body, '\n')
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	// Keep replies finitely framed through the streaming nx-baas proxy, including
	// login envelopes larger than net/http's automatic Content-Length buffer.
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	_, _ = w.Write(body)
}
func success(w http.ResponseWriter, data any) {
	writeJSON(w, 200, map[string]any{"code": 200, "status": "OK", "data": data})
}
func failure(w http.ResponseWriter, status int, name string, code int, message string) {
	writeJSON(w, status, map[string]any{"code": status, "status": http.StatusText(status), "error": name, "errorCode": code, "errorMessage": message})
}
func unavailable(w http.ResponseWriter) {
	failure(w, 503, "ServiceUnavailable", 1123, "This PlayFab operation is not available on OpenPak.")
}

type responseStatus struct {
	http.ResponseWriter
	code int
}

func (w *responseStatus) WriteHeader(code int) { w.code = code; w.ResponseWriter.WriteHeader(code) }

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &responseStatus{w, 200}
	// Never record headers, bodies, queries, or arbitrary path values. The next
	// unsupported SDK call is observable by its fixed API family/operation only.
	op := logOperation(r.URL.Path)
	host, _, _ := strings.Cut(r.Host, ":")
	cfg, ok := s.titles[strings.ToLower(host)]
	defer func() {
		title := strings.ToLower(host)
		if ok {
			title = cfg.Title
		}
		log.Printf("[playfab] title=%s operation=%s -> %d", title, op, rec.code)
	}()
	if !ok {
		unavailable(rec)
		return
	}
	if r.Method != http.MethodPost {
		rec.Header().Set("Allow", "POST")
		failure(rec, 405, "InvalidParams", 1000, "Use POST.")
		return
	}
	switch r.URL.Path {
	case "/Client/LoginWithNintendoServiceAccount":
		s.login(rec, r, cfg)
	case "/Authentication/GetEntityToken":
		s.getEntityToken(rec, r, cfg)
	case "/Event/WriteTelemetryEvents":
		s.observePartyTelemetry(rec, r)
	case "/MultiplayerServer/ListPartyQosServers":
		s.listPartyQosServers(rec, r)
	default:
		unavailable(rec)
	}
}

func logOperation(path string) string {
	// SDK routes are two short ASCII identifiers. Everything else may contain
	// an identifier or credential and is deliberately not logged.
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		return "unsupported"
	}
	switch parts[1] {
	case "Client", "Authentication", "MultiplayerServer", "Lobby", "Match", "Party", "CloudScript", "Data", "Profile", "Event", "Events":
	default:
		return "unsupported"
	}
	if len(parts[2]) == 0 || len(parts[2]) > 64 {
		return "unsupported"
	}
	for _, c := range parts[2] {
		if c < 'A' || c > 'Z' {
			if c < 'a' || c > 'z' {
				return "unsupported"
			}
		}
	}
	return path
}

func decodeBody(w http.ResponseWriter, r *http.Request, out any) bool {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			failure(w, 413, "InvalidParams", 1000, "Request body too large.")
		} else {
			failure(w, 400, "InvalidParams", 1000, "Invalid request body.")
		}
		return false
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' || json.Unmarshal(b, out) != nil {
		failure(w, 400, "InvalidParams", 1000, "Expected one JSON object.")
		return false
	}
	return true
}

func identityFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errLocked):
		failure(w, 403, "AccountBanned", 1002, "Account is locked.")
	case errors.Is(err, errIdentity):
		failure(w, 401, "NotAuthenticated", 1074, "Identity was rejected.")
	default:
		unavailable(w)
	}
}

func (s *server) login(w http.ResponseWriter, r *http.Request, cfg titleConfig) {
	var in loginRequest
	if !decodeBody(w, r, &in) {
		return
	}
	if in.TitleID != cfg.Title {
		failure(w, 400, "InvalidTitleId", 1004, "TitleId does not match this host.")
		return
	}
	if in.IdentityToken == "" || len(in.IdentityToken) > 6144 || in.EncryptedRequest != "" || in.PlayerSecret != "" {
		failure(w, 400, "InvalidParams", 1000, "A Nintendo service-account IdentityToken is required; encrypted requests and player secrets are unsupported.")
		return
	}
	id, err := s.ids.lookup(r.Context(), map[string]string{"id_token": in.IdentityToken})
	if err != nil {
		identityFailure(w, err)
		return
	}
	if !strings.EqualFold(id.ApplicationID, cfg.Application) {
		failure(w, 400, "InvalidTitleId", 1004, "Nintendo token is not scoped to this title.")
		return
	}
	now := s.now().UTC()
	p, created, err := s.state.login(cfg.Title, id.AccountID, in.CreateAccount, now)
	if errors.Is(err, errNoAccount) {
		failure(w, 400, "AccountNotFound", 1001, "No local title player exists; use CreateAccount.")
		return
	}
	if err != nil {
		unavailable(w)
		return
	}
	ticket, _, err := s.issue(p, id, sessionAudience, now)
	if err != nil {
		unavailable(w)
		return
	}
	entity, err := s.entityToken(p, id, now)
	if err != nil {
		unavailable(w)
		return
	}
	lastLogin := p.LastLogin
	if lastLogin.IsZero() {
		lastLogin = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	result := loginResult{PlayFabID: p.PlayFabID, SessionTicket: ticket, EntityToken: entity, NewlyCreated: created, LastLoginTime: lastLogin,
		SettingsForUser: map[string]bool{"GatherDeviceInfo": false, "GatherFocusInfo": false, "NeedsAttribution": false}}
	if in.InfoRequestParameters != nil {
		result.InfoResultPayload = combinedInfo(*in.InfoRequestParameters, p, id, created, now)
	}
	success(w, result)
}

func (s *server) entityToken(p titlePlayer, id identity, now time.Time) (entityResult, error) {
	token, expiry, err := s.issue(p, id, entityAudience, now)
	return entityResult{entityKey{p.EntityID, "title_player_account"}, token, expiry}, err
}

func (s *server) getEntityToken(w http.ResponseWriter, r *http.Request, cfg titleConfig) {
	var in struct {
		Entity *entityKey `json:"Entity"` // older SDKs optionally request a specific entity
	}
	if !decodeBody(w, r, &in) {
		return
	}
	tickets, entities := r.Header.Values("X-Authentication"), r.Header.Values("X-EntityToken")
	if len(tickets)+len(entities) != 1 || len(r.Header.Values("X-SecretKey")) != 0 {
		failure(w, 401, "NotAuthenticated", 1074, "Supply one session ticket or entity token.")
		return
	}
	token, audience := "", sessionAudience
	if len(tickets) == 1 {
		token = tickets[0]
	} else {
		token, audience = entities[0], entityAudience
	}
	now := s.now().UTC()
	c, err := s.verify(token, audience, cfg.Title, now)
	if err != nil {
		failure(w, 401, "NotAuthenticated", 1074, "Credential is invalid or expired.")
		return
	}
	if in.Entity != nil && (in.Entity.ID != c.EntityID || in.Entity.Type != "title_player_account") {
		failure(w, 403, "NotAuthenticated", 1074, "Requested entity does not match this credential.")
		return
	}
	id, err := s.ids.lookup(r.Context(), map[string]uint64{"pid": c.PID})
	if err != nil {
		identityFailure(w, err)
		return
	}
	if id.AccountID != c.AccountID || id.BaasUserID != c.BaasUserID || id.PID != c.PID {
		failure(w, 401, "NotAuthenticated", 1074, "Linked identity changed.")
		return
	}
	p, _ := s.state.player(cfg.Title, c.AccountID)
	result, err := s.entityToken(p, id, now)
	if err != nil {
		unavailable(w)
		return
	}
	success(w, result)
}
