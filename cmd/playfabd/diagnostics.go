package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// This is a bounded diagnostic observer, not a telemetry write implementation.
// Keep 503 until events can actually be persisted according to the contract.
// Never retain or print arbitrary payloads, event-name strings or credentials.
func (s *server) observePartyTelemetry(w http.ResponseWriter, r *http.Request) {
	headers := r.Header.Values("X-EntityToken")
	if len(headers) != 1 {
		failure(w, 401, "NotAuthenticated", 1074, "An entity token is required.")
		return
	}
	c, err := s.verify(headers[0], entityAudience, coreKeeperTitle, s.now())
	if err != nil {
		failure(w, 401, "NotAuthenticated", 1074, "Credential is invalid or expired.")
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
	var batch struct {
		Events []struct {
			Name        string
			Payload     json.RawMessage
			PayloadJSON string
		}
	}
	if !decodeBody(w, r, &batch) {
		return
	}
	if len(batch.Events) == 0 || len(batch.Events) > 200 {
		failure(w, 400, "InvalidParams", 1000, "Expected 1 to 200 events.")
		return
	}
	for _, event := range batch.Events {
		payload := event.Payload
		if len(payload) == 0 && event.PayloadJSON != "" {
			payload = []byte(event.PayloadJSON)
		}
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(payload)))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			continue
		}
		codes := diagnosticCodes(value, 0)
		sort.Strings(codes)
		// Names are hashed because events may contain client-supplied identity
		// strings. Only named numeric error/result fields are emitted.
		digest := sha256.Sum256([]byte(event.Name))
		log.Printf("[playfab] Party telemetry event=~%x diagnostics=%v", digest[:6], codes)
	}
	unavailable(w)
}

func diagnosticCodes(value any, depth int) []string {
	if depth > 8 {
		return nil
	}
	var out []string
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			name := strings.ToLower(strings.ReplaceAll(key, "_", ""))
			switch name {
			case "errorcode", "errordetail", "result", "resultcode", "statuscode", "hresult":
				if number, ok := child.(json.Number); ok {
					if code, err := strconv.ParseInt(string(number), 10, 64); err == nil && code >= -2147483648 && code <= 4294967295 {
						out = append(out, fmt.Sprintf("%s=%d", name, code))
					}
				}
			}
			out = append(out, diagnosticCodes(child, depth+1)...)
			if len(out) >= 32 {
				return out[:32]
			}
		}
	case []any:
		for _, child := range v {
			out = append(out, diagnosticCodes(child, depth+1)...)
			if len(out) >= 32 {
				return out[:32]
			}
		}
	}
	return out
}
