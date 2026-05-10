package internal

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "turso.tech/database/tursogo"
)

var db *sql.DB

func InitializeDB(root string) error {
	if db != nil {
		return nil
	}

	dir := filepath.Join(root, ".chronicle")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create .chronicle dir: %w", err)
	}

	dbPath := filepath.Join(dir, "history.db")
	var err error
	db, err = sql.Open("turso", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS changes (
		file_path VARCHAR,
		sha VARCHAR,
		previous VARCHAR,
		data TEXT,
		created_at INTEGER
	)`)
	if err != nil {
		return fmt.Errorf("create changes table: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_changes_file_path ON changes(file_path)`)
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_changes_sha ON changes(sha)`)
	if err != nil {
		return fmt.Errorf("create sha index: %w", err)
	}

	return nil
}

type RecentChange struct {
	FilePath  string
	CreatedAt int64
}

type FileChange struct {
	FilePath  string
	SHA       string
	Previous  string
	Data      string
	CreatedAt int64
}

func GetFileHistory(filePath string, limit int) ([]FileChange, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := db.Query(
		`SELECT file_path, sha, previous, data, created_at FROM changes
		 WHERE file_path = ?
		 ORDER BY created_at DESC
		 LIMIT ?`, filePath, limit)
	if err != nil {
		return nil, fmt.Errorf("query file history: %w", err)
	}
	defer rows.Close()

	var changes []FileChange
	for rows.Next() {
		var c FileChange
		var prev sql.NullString
		if err := rows.Scan(&c.FilePath, &c.SHA, &prev, &c.Data, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan file change: %w", err)
		}
		if prev.Valid {
			c.Previous = prev.String
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return changes, nil
}

func GetRecentChanges(limit int) ([]RecentChange, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := db.Query(
		`SELECT file_path, MAX(created_at) FROM changes
		 GROUP BY file_path
		 ORDER BY MAX(created_at) DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent changes: %w", err)
	}
	defer rows.Close()

	var changes []RecentChange
	for rows.Next() {
		var c RecentChange
		if err := rows.Scan(&c.FilePath, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent change: %w", err)
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return changes, nil
}

func AddChange(filePath, data string) (string, error) {
	sha := fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
	createdAt := time.Now().UnixMilli()

	var previous sql.NullString
	err := db.QueryRow(`SELECT sha FROM changes WHERE file_path = ? ORDER BY created_at DESC LIMIT 1`,
		filePath).Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("lookup previous sha: %w", err)
	}

	if previous.Valid && previous.String == sha {
		return "", nil
	}

	_, err = db.Exec(`INSERT INTO changes (file_path, sha, previous, data, created_at) VALUES (?, ?, ?, ?, ?)`,
		filePath, sha, previous, data, createdAt)
	if err != nil {
		return "", fmt.Errorf("insert change: %w", err)
	}

	return sha, nil
}
