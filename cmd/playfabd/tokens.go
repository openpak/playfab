package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	tokenIssuer     = "openpak/playfab"
	sessionAudience = "playfab/client"
	entityAudience  = "playfab/entity"
	tokenLifetime   = time.Hour
)

type credential struct {
	Issuer     string `json:"iss"`
	Audience   string `json:"aud"`
	Subject    string `json:"sub"`
	TitleID    string `json:"title"`
	AccountID  string `json:"account"`
	EntityID   string `json:"entity"`
	PID        uint64 `json:"pid"`
	BaasUserID string `json:"baas_user_id"`
	IssuedAt   int64  `json:"iat"`
	ExpiresAt  int64  `json:"exp"`
	Nonce      string `json:"jti"`
}

// Local opaque credentials, not Nintendo or Microsoft JWTs. Both types have
// independent prefixes and audiences; a session ticket cannot be an entity token.
func tokenPrefix(audience string) string {
	if audience == sessionAudience {
		return "opst1"
	}
	return "opet1"
}

func (s *server) issue(p titlePlayer, id identity, audience string, now time.Time) (string, time.Time, error) {
	expiry := now.Truncate(time.Second).Add(tokenLifetime)
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", time.Time{}, err
	}
	c := credential{tokenIssuer, audience, p.PlayFabID, p.TitleID, p.AccountID, p.EntityID, id.PID, id.BaasUserID, now.Unix(), expiry.Unix(), hex.EncodeToString(nonce)}
	b, err := json.Marshal(c)
	if err != nil {
		return "", time.Time{}, err
	}
	input := tokenPrefix(audience) + "." + base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, s.state.signingKey())
	mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), expiry, nil
}

func (s *server) verify(token, audience, title string, now time.Time) (credential, error) {
	var c credential
	bad := errors.New("invalid credential")
	if len(token) > 8192 {
		return c, bad
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != tokenPrefix(audience) {
		return c, bad
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return c, bad
	}
	mac := hmac.New(sha256.New, s.state.signingKey())
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, bad
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, bad
	}
	if c.Issuer != tokenIssuer || c.Audience != audience || c.TitleID != title || c.ExpiresAt <= now.Unix() || c.IssuedAt > now.Unix() || c.ExpiresAt <= c.IssuedAt || c.ExpiresAt-c.IssuedAt > int64(tokenLifetime/time.Second) || c.PID == 0 || c.BaasUserID == "" || c.Nonce == "" {
		return credential{}, bad
	}
	p, ok := s.state.player(title, c.AccountID)
	if !ok || c.Subject != p.PlayFabID || c.EntityID != p.EntityID {
		return credential{}, bad
	}
	return c, nil
}
