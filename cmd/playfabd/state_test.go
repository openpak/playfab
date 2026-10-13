package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRefusesCorruptionAndConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := openStore(dir); err == nil {
		other.close()
		t.Fatal("two writers accepted")
	}
	info, err := os.Stat(filepath.Join(dir, "playfab.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("state is not private")
	}
	s.close()
	if err := os.WriteFile(filepath.Join(dir, "playfab.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := openStore(dir); err == nil {
		s.close()
		t.Fatal("corrupt state silently regenerated")
	}
}

func TestFailedStateWriteDoesNotCreatePlayer(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	original := s.path
	s.path = filepath.Join(t.TempDir(), "missing", "state.json")
	if _, _, err := s.login("94845", "a", true, time.Now()); err == nil {
		t.Fatal("write should fail")
	}
	if len(s.Players) != 0 {
		t.Fatal("failed write changed in-memory mapping")
	}
	s.path = original
	p, created, err := s.login("94845", "a", true, time.Now())
	if err != nil || !created {
		t.Fatal("retry did not create mapping")
	}
	other, _, err := s.login("94845", "b", true, time.Now())
	if err != nil || p.PlayFabID == other.PlayFabID || p.EntityID == other.EntityID {
		t.Fatal("accounts share identity")
	}
}
