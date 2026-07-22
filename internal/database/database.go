package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Driver string

const (
	SQLite Driver = "sqlite"
)

type Config struct {
	URL string
}

func ParseConnectionURL(value string) (Driver, string, error) {
	value = strings.TrimSpace(value)
	scheme, raw, ok := strings.Cut(value, "://")
	if !ok {
		return "", "", fmt.Errorf("database URL %q must use sqlite://", value)
	}
	switch strings.ToLower(scheme) {
	case "sqlite", "sqlite3":
		if raw == "" {
			return "", "", errors.New("sqlite database path is required")
		}
		path, query, _ := strings.Cut(raw, "?")
		decoded, err := url.PathUnescape(path)
		if err != nil {
			return "", "", fmt.Errorf("decode sqlite path: %w", err)
		}
		if query != "" {
			decoded += "?" + query
		}
		return SQLite, decoded, nil
	default:
		return "", "", fmt.Errorf("unsupported database URL scheme %q", scheme)
	}
}

type DB struct {
	db     *sql.DB
	driver Driver
}

func Open(cfg Config) (*DB, error) {
	driver, dsn, err := ParseConnectionURL(cfg.URL)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(string(driver), dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{db: db, driver: driver}, nil
}

func (d *DB) Driver() Driver {
	return d.driver
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) PingContext(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(ctx, d.query(query), args...)
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, d.query(query), args...)
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, d.query(query), args...)
}

func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, driver: d.driver}, nil
}

func (d *DB) query(query string) string {
	return adaptQuery(d.driver, query)
}

type Tx struct {
	tx     *sql.Tx
	driver Driver
}

func (tx *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.tx.ExecContext(ctx, adaptQuery(tx.driver, query), args...)
}

func (tx *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return tx.tx.QueryContext(ctx, adaptQuery(tx.driver, query), args...)
}

func (tx *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return tx.tx.QueryRowContext(ctx, adaptQuery(tx.driver, query), args...)
}

func (tx *Tx) Commit() error {
	return tx.tx.Commit()
}

func (tx *Tx) Rollback() error {
	return tx.tx.Rollback()
}

var (
	forUpdatePattern = regexp.MustCompile(`(?i)\s+FOR\s+UPDATE\s*;?\s*$`)
)

func adaptQuery(driver Driver, query string) string {
	return forUpdatePattern.ReplaceAllString(query, "")
}

func IsDuplicate(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed")
}

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
