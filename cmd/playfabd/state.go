package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// legacyState is the old playfab.json.
type legacyState struct {
	Version    int                    `json:"version"`
	SigningKey []byte                 `json:"signing_key"`
	Players    map[string]titlePlayer `json:"players"`
}

// store keeps the token signing key and the title-player mappings in Postgres.
type store struct {
	pool *pgxpool.Pool
	key  []byte // immutable once loaded
}

const schema = `
CREATE TABLE IF NOT EXISTS playfab_meta (
  name  text PRIMARY KEY,
  value bytea NOT NULL
);
CREATE TABLE IF NOT EXISTS playfab_players (
  title_id    text NOT NULL,
  account_id  text NOT NULL,
  playfab_id  text NOT NULL,
  entity_id   text NOT NULL,
  created     timestamptz NOT NULL,
  last_login  timestamptz NOT NULL,
  PRIMARY KEY (title_id, account_id)
);`

// openStore connects (small pool), creates the schema, imports playfab.json from importDir
// once if present (key value kept identical, file left in place) and loads the signing key.
// A key is generated only when neither the database nor the legacy file has one, and an
// existing key is never replaced.
func openStore(ctx context.Context, url, importDir string) (*store, error) {
	if url == "" {
		return nil, errors.New("PLAYFAB_DATABASE_URL is not set: player state lives in Postgres")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 3
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &store{pool: pool}
	ok := false
	defer func() {
		if !ok {
			pool.Close()
		}
	}()
	if _, err := pool.Exec(ctx, schema); err != nil {
		return nil, err
	}
	if importDir != "" {
		if err := s.importLegacy(ctx, filepath.Join(importDir, "playfab.json")); err != nil {
			return nil, fmt.Errorf("import playfab.json: %w", err)
		}
	}
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, `INSERT INTO playfab_meta (name,value) VALUES ('signing_key',$1) ON CONFLICT DO NOTHING`, fresh); err != nil {
		return nil, err
	}
	if err := pool.QueryRow(ctx, `SELECT value FROM playfab_meta WHERE name='signing_key'`).Scan(&s.key); err != nil {
		return nil, err
	}
	if len(s.key) != 32 {
		return nil, errors.New("invalid PlayFab signing key in database")
	}
	ok = true
	return s, nil
}

func (s *store) close() error { s.pool.Close(); return nil }

func (s *store) signingKey() []byte { return append([]byte(nil), s.key...) }

func playerKey(title, account string) string { return title + ":" + account }

// importLegacy copies the signing key and players of playfab.json. Idempotent; never
// overwrites a key already in the database.
func (s *store) importLegacy(ctx context.Context, path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var st legacyState
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	if st.Version != 1 || len(st.SigningKey) != 32 || st.Players == nil {
		return errors.New("invalid PlayFab state file")
	}
	for key, p := range st.Players {
		if key != playerKey(p.TitleID, p.AccountID) || p.AccountID == "" || p.TitleID == "" || p.PlayFabID == "" || p.EntityID == "" || p.Created.IsZero() {
			return errors.New("invalid title-player mapping")
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	kt, err := tx.Exec(ctx, `INSERT INTO playfab_meta (name,value) VALUES ('signing_key',$1) ON CONFLICT DO NOTHING`, st.SigningKey)
	if err != nil {
		return err
	}
	added := 0
	for _, p := range st.Players {
		ct, err := tx.Exec(ctx, `INSERT INTO playfab_players (title_id,account_id,playfab_id,entity_id,created,last_login)
VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, p.TitleID, p.AccountID, p.PlayFabID, p.EntityID, p.Created, p.LastLogin)
		if err != nil {
			return err
		}
		added += int(ct.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	var n int
	s.pool.QueryRow(ctx, `SELECT count(*) FROM playfab_players`).Scan(&n)
	log.Printf("[playfab] postgres store: import of %s: signing key %s, %d/%d players added; table holds %d players",
		path, map[bool]string{true: "imported", false: "already in database (kept)"}[kt.RowsAffected() == 1], added, len(st.Players), n)
	return nil
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}

const playerCols = `title_id,account_id,playfab_id,entity_id,created,last_login`

func scanPlayer(row pgx.Row) (titlePlayer, error) {
	var p titlePlayer
	err := row.Scan(&p.TitleID, &p.AccountID, &p.PlayFabID, &p.EntityID, &p.Created, &p.LastLogin)
	p.Created, p.LastLogin = p.Created.UTC(), p.LastLogin.UTC()
	return p, err
}

func (s *store) login(title, account string, create bool, now time.Time) (titlePlayer, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := scanPlayer(s.pool.QueryRow(ctx, `SELECT `+playerCols+` FROM playfab_players WHERE title_id=$1 AND account_id=$2`, title, account))
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, false, err
	}
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
		ct, err := s.pool.Exec(ctx, `INSERT INTO playfab_players (`+playerCols+`) VALUES ($1,$2,$3,$4,$5,$5) ON CONFLICT DO NOTHING`, title, account, id, entity, now)
		if err != nil {
			return p, false, err
		}
		// A concurrent first login may have inserted it: then this call is not the creator.
		p, err = scanPlayer(s.pool.QueryRow(ctx, `SELECT `+playerCols+` FROM playfab_players WHERE title_id=$1 AND account_id=$2`, title, account))
		if err != nil {
			return p, false, err
		}
		created := ct.RowsAffected() == 1
		// Callers report the pre-login view: LastLogin of a new player is "now" only after this call.
		if created {
			p.LastLogin = time.Time{}
		}
		if _, err := s.pool.Exec(ctx, `UPDATE playfab_players SET last_login=$3 WHERE title_id=$1 AND account_id=$2`, title, account, now); err != nil {
			return p, false, err
		}
		return p, created, nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE playfab_players SET last_login=$3 WHERE title_id=$1 AND account_id=$2`, title, account, now); err != nil {
		return p, false, err
	}
	return p, false, nil
}

func (s *store) player(title, account string) (titlePlayer, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := scanPlayer(s.pool.QueryRow(ctx, `SELECT `+playerCols+` FROM playfab_players WHERE title_id=$1 AND account_id=$2`, title, account))
	return p, err == nil
}

func (s *store) count() int {
	var n int
	s.pool.QueryRow(context.Background(), `SELECT count(*) FROM playfab_players`).Scan(&n)
	return n
}
