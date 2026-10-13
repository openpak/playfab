package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

var errNoAccount = errors.New("title player not found")

type titlePlayer struct {
	AccountID string    `json:"account_id"`
	TitleID   string    `json:"title_id"`
	PlayFabID string    `json:"playfab_id"`
	EntityID  string    `json:"entity_id"`
	Created   time.Time `json:"created"`
	LastLogin time.Time `json:"last_login"`
}

type diskState struct {
	Version    int                    `json:"version"`
	SigningKey []byte                 `json:"signing_key"`
	Players    map[string]titlePlayer `json:"players"`
}

type store struct {
	mu   sync.Mutex
	path string
	lock *os.File
	diskState
}

// A single private file keeps the key and identity mappings together. Never
// regenerate credentials when state exists but cannot be read or validated.
func openStore(dir string) (*store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("state already in use: %w", err)
	}
	s := &store{path: filepath.Join(dir, "playfab.json"), lock: lock}
	ok := false
	defer func() {
		if !ok {
			s.close()
		}
	}()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.diskState = diskState{Version: 1, SigningKey: make([]byte, 32), Players: map[string]titlePlayer{}}
		if _, err := rand.Read(s.SigningKey); err != nil {
			return nil, err
		}
		if err := s.save(s.diskState); err != nil {
			return nil, err
		}
	} else {
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &s.diskState); err != nil {
			return nil, err
		}
		if s.Version != 1 || len(s.SigningKey) != 32 || s.Players == nil {
			return nil, errors.New("invalid PlayFab state")
		}
		for key, p := range s.Players {
			if key != playerKey(p.TitleID, p.AccountID) || p.AccountID == "" || p.TitleID == "" || p.PlayFabID == "" || p.EntityID == "" || p.Created.IsZero() {
				return nil, errors.New("invalid title-player mapping")
			}
		}
	}
	ok = true
	return s, nil
}

func (s *store) close() error { return s.lock.Close() }
func (s *store) signingKey() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.SigningKey...)
}
func playerKey(title, account string) string { return title + ":" + account }

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}

func (s *store) save(state diskState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".playfab-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (s *store) login(title, account string, create bool, now time.Time) (titlePlayer, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := playerKey(title, account)
	p, exists := s.Players[key]
	if !exists {
		if !create {
			return p, false, errNoAccount
		}
		id, err := randomID()
		if err != nil {
			return p, false, err
		}
		entity, err := randomID()
		if err != nil {
			return p, false, err
		}
		p = titlePlayer{AccountID: account, TitleID: title, PlayFabID: id, EntityID: entity, Created: now}
	}
	// Commit a copy; a failed write must not turn a retry into NewlyCreated=false.
	next := s.diskState
	next.Players = make(map[string]titlePlayer, len(s.Players)+1)
	for k, v := range s.Players {
		next.Players[k] = v
	}
	updated := p
	updated.LastLogin = now
	next.Players[key] = updated
	if err := s.save(next); err != nil {
		return p, false, err
	}
	s.diskState = next
	return p, !exists, nil
}

func (s *store) player(title, account string) (titlePlayer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.Players[playerKey(title, account)]
	return p, ok
}
