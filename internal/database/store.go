package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Store struct {
	db *DB
	mu sync.Mutex
}

// Open opens the bot's private KV database. The database file and its parent
// directory are created automatically when they do not exist.
func OpenStore(path string) (*Store, error) {
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}
	db, err := Open(Config{URL: "sqlite://" + path})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS kv_namespaces (
	namespace TEXT PRIMARY KEY,
	created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
DROP TABLE IF EXISTS kv_binding;
DELETE FROM kv_namespaces WHERE namespace='binding';
`)
	return err
}

func (s *Store) KV(namespace string) (*KV, error) {
	if !validNamespace.MatchString(namespace) {
		return nil, fmt.Errorf("invalid kv namespace %q", namespace)
	}
	table := "kv_" + namespace
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(context.Background(), `INSERT OR IGNORE INTO kv_namespaces(namespace, created_at) VALUES (?, ?)`, namespace, time.Now().Unix()); err != nil {
		return nil, err
	}
	_, err := s.db.ExecContext(context.Background(), fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	`+"`key`"+` TEXT PRIMARY KEY,
	`+"`value`"+` TEXT NOT NULL
);
`, table))
	if err != nil {
		return nil, err
	}
	if err := s.normalizeKVTable(table); err != nil {
		return nil, err
	}
	return &KV{db: s.db, table: table}, nil
}

// Get reads a value using a key path. The first key is the namespace and the
// remaining keys form the value key, for example:
//
//	db.Get(ctx, []string{"plugin_name", "123", "ticket"}, &value)
//
// Namespaces and tables are created automatically by the store.
func (s *Store) Get(ctx context.Context, keys []string, out any) (bool, error) {
	kv, key, err := s.resolveKeys(keys)
	if err != nil {
		return false, err
	}
	return kv.Get(ctx, key, out)
}

// Set writes a value using a key path. Values are serialized as JSON so the
// same interface supports strings, numbers, structs, slices and maps.
func (s *Store) Set(ctx context.Context, keys []string, value any) error {
	kv, key, err := s.resolveKeys(keys)
	if err != nil {
		return err
	}
	return kv.Set(ctx, key, value)
}

func (s *Store) SetNX(ctx context.Context, keys []string, value any) (bool, error) {
	kv, key, err := s.resolveKeys(keys)
	if err != nil {
		return false, err
	}
	return kv.SetNX(ctx, key, value)
}

func (s *Store) Delete(ctx context.Context, keys []string) (bool, error) {
	kv, key, err := s.resolveKeys(keys)
	if err != nil {
		return false, err
	}
	return kv.Delete(ctx, key)
}

func (s *Store) All(ctx context.Context, namespace string) (map[string]json.RawMessage, error) {
	kv, err := s.KV(namespace)
	if err != nil {
		return nil, err
	}
	return kv.All(ctx)
}

func (s *Store) resolveKeys(keys []string) (*KV, string, error) {
	if len(keys) < 2 {
		return nil, "", fmt.Errorf("kv key path must contain namespace and key")
	}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			return nil, "", fmt.Errorf("kv key path contains an empty key")
		}
	}
	kv, err := s.KV(keys[0])
	if err != nil {
		return nil, "", err
	}
	return kv, strings.Join(keys[1:], "\x1f"), nil
}

var validNamespace = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

func (s *Store) normalizeKVTable(table string) error {
	rows, err := s.db.QueryContext(context.Background(), fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	columns := map[string]bool{}
	count := 0
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		columns[name] = true
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !columns["key"] || !columns["value"] {
		return fmt.Errorf("kv table %s is missing key/value columns", table)
	}
	if count == 2 {
		return nil
	}

	tmp := table + "_compact"
	_, err = s.db.ExecContext(context.Background(), fmt.Sprintf(`
DROP TABLE IF EXISTS %s;
CREATE TABLE %s (
	`+"`key`"+` TEXT PRIMARY KEY,
	`+"`value`"+` TEXT NOT NULL
);
INSERT OR REPLACE INTO %s(`+"`key`, `value`"+`) SELECT `+"`key`, `value`"+` FROM %s;
DROP TABLE %s;
ALTER TABLE %s RENAME TO %s;
`, tmp, tmp, tmp, table, table, tmp, table))
	return err
}

type KV struct {
	db    *DB
	table string
}

func (kv *KV) Get(ctx context.Context, key string, out any) (bool, error) {
	row := kv.db.QueryRowContext(ctx, fmt.Sprintf("SELECT `value` FROM %s WHERE `key` = ?", kv.table), key)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), out)
}

func (kv *KV) Set(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`
INSERT INTO %s(`+"`key`, `value`"+`) VALUES (?, ?)
ON CONFLICT(`+"`key`"+`) DO UPDATE SET `+"`value`"+`=excluded.`+"`value`"+`
`, kv.table)
	_, err = kv.db.ExecContext(ctx, query, key, string(raw))
	return err
}

func (kv *KV) SetNX(ctx context.Context, key string, value any) (bool, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	res, err := kv.db.ExecContext(ctx, fmt.Sprintf(`
INSERT OR IGNORE INTO %s(`+"`key`, `value`"+`) VALUES (?, ?)
`, kv.table), key, string(raw))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (kv *KV) Delete(ctx context.Context, key string) (bool, error) {
	res, err := kv.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE `key` = ?", kv.table), key)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (kv *KV) All(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := kv.db.QueryContext(ctx, fmt.Sprintf("SELECT `key`, `value` FROM %s", kv.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		out[key] = json.RawMessage(raw)
	}
	return out, rows.Err()
}
