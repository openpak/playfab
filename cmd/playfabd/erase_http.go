package main

// Account erasure plumbing (card openpak/prds#120): the HTTP handler for
// DELETE /internal/accounts/{account_id}/data. This service keys its rows by the OpenPak account id
// itself (nx-baas hands it the core account id at login), so no core lookup is needed.

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
)

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// eraseHandler serves DELETE /internal/accounts/{account_id}/data with Bearer ACCOUNT_ERASE_KEY.
// The key is read per request; unset means the endpoint answers 503, never open.
func eraseHandler(service string, erase func(ctx context.Context, account string) (int, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := os.Getenv("ACCOUNT_ERASE_KEY")
		if key == "" {
			http.Error(w, "erase not configured", http.StatusServiceUnavailable)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(key)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/internal/accounts/"), "/data")
		if !uuidRE.MatchString(id) {
			http.Error(w, "bad account id", http.StatusBadRequest)
			return
		}
		id = strings.ToLower(id)
		n, err := erase(r.Context(), id)
		if err != nil {
			log.Printf("ERROR %s erase failed: %v", service, err)
			http.Error(w, "erase failed", http.StatusInternalServerError)
			return
		}
		log.Printf("account erased account=%s rows=%d service=%s", id, n, service)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"` + service + `","rows":` + itoaErase(n) + `}` + "\n"))
	}
}

func itoaErase(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for ; n > 0; n /= 10 {
		i--
		b[i] = byte('0' + n%10)
	}
	return string(b[i:])
}
