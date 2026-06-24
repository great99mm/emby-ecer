package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteStateSchemaVersion = 1

var stateDB *sqliteStateStore

type sqliteStateStore struct {
	db *sql.DB
}

func initSQLiteState(path string) (*sqliteStateStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("SQLite path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	store := &sqliteStateStore{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	log.Printf("SQLite state store ready: %s", path)
	return store, nil
}

func (s *sqliteStateStore) initSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS app_state (
  key TEXT PRIMARY KEY,
  value BLOB NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS schema_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT INTO schema_meta(key, value) VALUES ('version', '1')
  ON CONFLICT(key) DO UPDATE SET value=excluded.value;
`)
	return err
}

func (s *sqliteStateStore) Load(key string, out any) error {
	if s == nil {
		return os.ErrNotExist
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_state WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return os.ErrNotExist
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (s *sqliteStateStore) Save(key string, value any) error {
	if s == nil {
		return errors.New("SQLite state store is not initialized")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = s.db.ExecContext(ctx, `
INSERT INTO app_state(key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at
`, key, raw, time.Now().Format(time.RFC3339Nano))
	return err
}

func (s *sqliteStateStore) ImportJSONFile(key, path string, out any) bool {
	if s == nil {
		return false
	}
	var existing any
	if err := s.Load(key, &existing); err == nil {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return false
	}
	if err := json.Unmarshal(raw, out); err != nil {
		log.Printf("SQLite migration skipped invalid JSON %s: %v", path, err)
		return false
	}
	if err := s.Save(key, out); err != nil {
		log.Printf("SQLite migration failed for %s: %v", path, err)
		return false
	}
	log.Printf("Migrated JSON state to SQLite: %s -> %s", path, key)
	return true
}

func sqlitePathFromConfig(configPath string) string {
	return getenv("SQLITE_PATH", filepath.Join(filepath.Dir(configPath), "emby-ecer.db"))
}

func loadStateJSON(key, legacyPath string, out any) error {
	if stateDB != nil {
		if err := stateDB.Load(key, out); err == nil {
			return nil
		}
	}
	raw, err := os.ReadFile(legacyPath)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func saveStateJSON(key, legacyPath string, value any) error {
	if stateDB != nil {
		return stateDB.Save(key, value)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(legacyPath, raw, 0o600)
}
