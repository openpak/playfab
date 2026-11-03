package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testDSN returns PLAYFAB_TEST_DATABASE_URL (or skips). Tables are emptied unless keep is set.
func testDSN(t *testing.T, keep ...bool) string {
	t.Helper()
	dsn := os.Getenv("PLAYFAB_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PLAYFAB_TEST_DATABASE_URL not set")
	}
	if len(keep) == 0 {
		st, err := openStore(context.Background(), dsn, "")
		if err != nil {
			t.Fatal(err)
		}
		st.pool.Exec(context.Background(), `TRUNCATE playfab_meta, playfab_players`)
		st.close()
	}
	return dsn
}

func TestStoreRequiresDatabase(t *testing.T) {
	if s, err := openStore(context.Background(), "", ""); err == nil {
		s.close()
		t.Fatal("opened without a database URL")
	}
}

// Key and players written by one instance are seen, unchanged, by the next.
func TestStateSurvivesRestart(t *testing.T) {
	dsn := testDSN(t)
	ctx := context.Background()
	s1, err := openStore(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	p, created, err := s1.login("94845", "a", true, now)
	if err != nil || !created || !p.LastLogin.IsZero() {
		t.Fatalf("first login: %+v %v %v", p, created, err)
	}
	key := s1.signingKey()
	s1.close()

	s2, err := openStore(ctx, dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.close()
	if !bytes.Equal(key, s2.signingKey()) {
		t.Fatal("signing key changed across restart")
	}
	q, created, err := s2.login("94845", "a", true, now.Add(time.Hour))
	if err != nil || created || q.PlayFabID != p.PlayFabID || q.EntityID != p.EntityID || !q.LastLogin.Equal(now) {
		t.Fatalf("player lost or changed: %+v created=%v err=%v", q, created, err)
	}
	if _, _, err := s2.login("94845", "nobody", false, now); err != errNoAccount {
		t.Fatalf("login without create: %v", err)
	}
	other, _, err := s2.login("94845", "b", true, now)
	if err != nil || p.PlayFabID == other.PlayFabID || p.EntityID == other.EntityID {
		t.Fatal("accounts share identity")
	}
}

func TestFailedWriteDoesNotCreatePlayer(t *testing.T) {
	dsn := testDSN(t)
	s, err := openStore(context.Background(), dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	s.pool.Close() // database unreachable
	if _, _, err := s.login("94845", "a", true, time.Now()); err == nil {
		t.Fatal("write should fail")
	}
	s2, err := openStore(context.Background(), dsn, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.close()
	if s2.count() != 0 {
		t.Fatal("failed write left a player")
	}
}

const legacyJSON = `{"version":1,"signing_key":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=","players":{
"94845:a":{"account_id":"a","title_id":"94845","playfab_id":"AAAA","entity_id":"BBBB","created":"2026-10-12T19:00:00Z","last_login":"2026-10-13T19:00:00Z"},
"94845:b":{"account_id":"b","title_id":"94845","playfab_id":"CCCC","entity_id":"DDDD","created":"2026-10-12T19:00:00Z","last_login":"0001-01-01T00:00:00Z"}}}`

func TestImportLegacyKeepsKeyAndIsIdempotent(t *testing.T) {
	dsn := testDSN(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "playfab.json")
	os.WriteFile(path, []byte(legacyJSON), 0o600)
	want := make([]byte, 32)
	for i := range want {
		want[i] = byte(i)
	}
	for i := 0; i < 2; i++ {
		s, err := openStore(context.Background(), dsn, dir)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(s.signingKey(), want) {
			t.Fatal("signing key differs from playfab.json")
		}
		if s.count() != 2 {
			t.Fatalf("players %d", s.count())
		}
		if p, ok := s.player("94845", "a"); !ok || p.PlayFabID != "AAAA" || p.LastLogin.Format(time.RFC3339) != "2026-10-13T19:00:00Z" {
			t.Fatalf("player a: %+v", p)
		}
		s.close()
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("playfab.json removed")
	}
}

func TestCorruptLegacyFileIsRefused(t *testing.T) {
	dsn := testDSN(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "playfab.json"), []byte(`{"version":1}`), 0o600)
	if s, err := openStore(context.Background(), dsn, dir); err == nil {
		s.close()
		t.Fatal("corrupt state silently replaced")
	}
}
