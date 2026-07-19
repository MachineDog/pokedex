package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type apiDatabase struct {
	db *sql.DB
}

func openAPIDatabase(path string) (*apiDatabase, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`CREATE TABLE IF NOT EXISTS api_resources (
			url TEXT PRIMARY KEY,
			resource_type TEXT NOT NULL DEFAULT '',
			resource_name TEXT NOT NULL DEFAULT '',
			body BLOB NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS api_resources_lookup ON api_resources(resource_type, resource_name)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize API database: %w", err)
		}
	}
	return &apiDatabase{db: db}, nil
}

func (d *apiDatabase) read(ctx context.Context, url string, ttl time.Duration) ([]byte, error) {
	var body []byte
	var updated int64
	err := d.db.QueryRowContext(ctx, `SELECT body, updated_at FROM api_resources WHERE url = ?`, url).Scan(&body, &updated)
	if err != nil {
		return nil, err
	}
	if ttl > 0 && time.Since(time.Unix(updated, 0)) >= ttl {
		return nil, errors.New("database entry expired")
	}
	return body, nil
}

func (d *apiDatabase) write(ctx context.Context, url, resourceType, resourceName string, body []byte) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO api_resources(url, resource_type, resource_name, body, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET resource_type=excluded.resource_type, resource_name=excluded.resource_name, body=excluded.body, updated_at=excluded.updated_at`,
		url, resourceType, resourceName, body, time.Now().Unix())
	return err
}

func (d *apiDatabase) close() error { return d.db.Close() }

func (d *apiDatabase) count() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT count(*) FROM api_resources`).Scan(&count)
	return count, err
}
