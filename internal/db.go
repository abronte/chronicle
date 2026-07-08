package internal

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "turso.tech/database/tursogo"
)

var db *sql.DB
var dbMu sync.Mutex

const (
	ChangeTypeModify = "modify"
	ChangeTypeDelete = "delete"
)

func InitializeDB(root string) error {
	if strings.TrimSpace(root) == "" {
		return InitializeCentralDB()
	}
	return InitializeDBAt(filepath.Join(root, historyName))
}

func InitializeCentralDB() error {
	dbPath, err := HistoryDBPath()
	if err != nil {
		return err
	}
	return InitializeDBAt(dbPath)
}

func InitializeDBAt(dbPath string) error {
	dbMu.Lock()
	defer dbMu.Unlock()

	if db != nil {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	var err error
	db, err = sql.Open("turso", localTursoDSN(dbPath))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS changes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		directory_path TEXT NOT NULL,
		file_path TEXT NOT NULL,
		absolute_path TEXT NOT NULL,
		sha TEXT NOT NULL,
		previous TEXT,
		data TEXT NOT NULL,
		change_type TEXT NOT NULL DEFAULT 'modify',
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("create changes table: %w", err)
	}
	if err := ensureChangesColumn("change_type", "change_type TEXT NOT NULL DEFAULT 'modify'"); err != nil {
		return err
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_changes_file_path ON changes(file_path)`)
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_changes_directory_file_created_at ON changes(directory_path, file_path, created_at)`)
	if err != nil {
		return fmt.Errorf("create directory file index: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_changes_directory_created_at ON changes(directory_path, created_at)`)
	if err != nil {
		return fmt.Errorf("create directory index: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_changes_sha ON changes(sha)`)
	if err != nil {
		return fmt.Errorf("create sha index: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_changes_absolute_path ON changes(absolute_path)`)
	if err != nil {
		return fmt.Errorf("create absolute path index: %w", err)
	}

	return nil
}

type RecentChange struct {
	DirectoryPath string
	FilePath      string
	AbsolutePath  string
	ChangeType    string
	CreatedAt     int64
}

type FileChange struct {
	ID            int64
	DirectoryPath string
	FilePath      string
	AbsolutePath  string
	SHA           string
	Previous      string
	Data          string
	ChangeType    string
	CreatedAt     int64
}

func GetFileHistory(filePath string, limit int) ([]FileChange, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	queryPath := normalizeQueryFilePath(filePath)
	rows, err := db.Query(
		`SELECT id, directory_path, file_path, absolute_path, sha, previous, data, change_type, created_at FROM changes
		 WHERE file_path = ? OR absolute_path = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`, queryPath, filePath, limit)
	if err != nil {
		return nil, fmt.Errorf("query file history: %w", err)
	}
	defer rows.Close()

	return scanFileChanges(rows)
}

func GetFileHistoryForDirectory(directoryPath, filePath string, limit int) ([]FileChange, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	dir, err := NormalizeDirectoryPath(directoryPath, false)
	if err != nil {
		return nil, err
	}
	rel, _, err := normalizeTrackedFilePath(dir, filePath)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(
		`SELECT id, directory_path, file_path, absolute_path, sha, previous, data, change_type, created_at FROM changes
		 WHERE directory_path = ? AND file_path = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`, dir, rel, limit)
	if err != nil {
		return nil, fmt.Errorf("query directory file history: %w", err)
	}
	defer rows.Close()

	return scanFileChanges(rows)
}

func GetRecentChanges(limit int) ([]RecentChange, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := db.Query(
		`SELECT directory_path, file_path, absolute_path, change_type, created_at FROM changes
		 WHERE id IN (
			SELECT MAX(id) FROM changes GROUP BY directory_path, file_path
		 )
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent changes: %w", err)
	}
	defer rows.Close()

	var changes []RecentChange
	for rows.Next() {
		var c RecentChange
		if err := rows.Scan(&c.DirectoryPath, &c.FilePath, &c.AbsolutePath, &c.ChangeType, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent change: %w", err)
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return changes, nil
}

func GetDirectoryChanges(directoryPath string, limit int) ([]FileChange, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	dir, err := NormalizeDirectoryPath(directoryPath, false)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(
		`SELECT id, directory_path, file_path, absolute_path, sha, previous, data, change_type, created_at FROM changes
		 WHERE directory_path = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`, dir, limit)
	if err != nil {
		return nil, fmt.Errorf("query directory changes: %w", err)
	}
	defer rows.Close()

	return scanFileChanges(rows)
}

func GetChangedFilesForDirectory(directoryPath string, limit int) ([]RecentChange, error) {
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	dir, err := NormalizeDirectoryPath(directoryPath, false)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(
		`SELECT directory_path, file_path, absolute_path, change_type, created_at FROM changes
		 WHERE directory_path = ? AND id IN (
			SELECT MAX(id) FROM changes WHERE directory_path = ? GROUP BY file_path
		 )
		 ORDER BY created_at DESC, file_path ASC
		 LIMIT ?`, dir, dir, limit)
	if err != nil {
		return nil, fmt.Errorf("query changed files: %w", err)
	}
	defer rows.Close()

	var changes []RecentChange
	for rows.Next() {
		var c RecentChange
		if err := rows.Scan(&c.DirectoryPath, &c.FilePath, &c.AbsolutePath, &c.ChangeType, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan changed file: %w", err)
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return changes, nil
}

func GetChangeByID(id int64) (FileChange, error) {
	if db == nil {
		return FileChange{}, fmt.Errorf("database not initialized")
	}

	row := db.QueryRow(
		`SELECT id, directory_path, file_path, absolute_path, sha, previous, data, change_type, created_at
		 FROM changes WHERE id = ?`, id)
	return scanFileChange(row)
}

func GetPreviousChange(change FileChange) (FileChange, bool, error) {
	if db == nil {
		return FileChange{}, false, fmt.Errorf("database not initialized")
	}

	row := db.QueryRow(
		`SELECT id, directory_path, file_path, absolute_path, sha, previous, data, change_type, created_at
		 FROM changes
		 WHERE directory_path = ? AND file_path = ? AND id < ?
		 ORDER BY id DESC
		 LIMIT 1`, change.DirectoryPath, change.FilePath, change.ID)

	prev, err := scanFileChange(row)
	if err == sql.ErrNoRows {
		return FileChange{}, false, nil
	}
	if err != nil {
		return FileChange{}, false, err
	}
	return prev, true, nil
}

func AddChange(filePath, data string) (string, error) {
	return AddChangeForDirectory(".", filePath, data)
}

func AddChangeForDirectory(directoryPath, filePath, data string) (string, error) {
	if db == nil {
		return "", fmt.Errorf("database not initialized")
	}

	dir, err := NormalizeDirectoryPath(directoryPath, false)
	if err != nil {
		return "", err
	}
	rel, abs, err := normalizeTrackedFilePath(dir, filePath)
	if err != nil {
		return "", err
	}

	sha := fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
	createdAt := time.Now().UnixMilli()

	latest, found, err := getLatestFileChange(dir, rel)
	if err != nil {
		return "", err
	}

	if found && latest.ChangeType == ChangeTypeModify && latest.SHA == sha {
		return "", nil
	}

	var prevValue any
	if found {
		prevValue = latest.SHA
	}

	_, err = db.Exec(
		`INSERT INTO changes (directory_path, file_path, absolute_path, sha, previous, data, change_type, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		dir, rel, abs, sha, prevValue, data, ChangeTypeModify, createdAt)
	if err != nil {
		return "", fmt.Errorf("insert change: %w", err)
	}

	return sha, nil
}

func AddDeleteForDirectory(directoryPath, filePath string) (string, error) {
	if db == nil {
		return "", fmt.Errorf("database not initialized")
	}

	dir, err := NormalizeDirectoryPath(directoryPath, false)
	if err != nil {
		return "", err
	}
	rel, abs, err := normalizeTrackedFilePath(dir, filePath)
	if err != nil {
		return "", err
	}

	latest, found, err := getLatestFileChange(dir, rel)
	if err != nil {
		return "", err
	}
	if !found || latest.ChangeType == ChangeTypeDelete {
		return "", nil
	}

	sha := deletionSHA(rel, latest.SHA)
	createdAt := time.Now().UnixMilli()
	_, err = db.Exec(
		`INSERT INTO changes (directory_path, file_path, absolute_path, sha, previous, data, change_type, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		dir, rel, abs, sha, latest.SHA, latest.Data, ChangeTypeDelete, createdAt)
	if err != nil {
		return "", fmt.Errorf("insert delete change: %w", err)
	}

	return sha, nil
}

func localTursoDSN(path string) string {
	return filepath.Clean(path)
}

func normalizeTrackedFilePath(directoryPath, filePath string) (string, string, error) {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return "", "", fmt.Errorf("file path required")
	}

	var abs string
	if filepath.IsAbs(filePath) {
		abs = filepath.Clean(filePath)
	} else {
		abs = filepath.Join(directoryPath, filepath.FromSlash(filePath))
	}

	rel, err := filepath.Rel(directoryPath, abs)
	if err != nil {
		return "", "", fmt.Errorf("resolve relative file path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("%s is not inside monitored directory %s", abs, directoryPath)
	}
	return filepath.ToSlash(filepath.Clean(rel)), abs, nil
}

func normalizeQueryFilePath(filePath string) string {
	if filepath.IsAbs(filePath) {
		return filepath.ToSlash(filepath.Clean(filePath))
	}
	return filepath.ToSlash(filepath.Clean(filePath))
}

func scanFileChanges(rows *sql.Rows) ([]FileChange, error) {
	var changes []FileChange
	for rows.Next() {
		c, err := scanFileChange(rows)
		if err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return changes, nil
}

type fileChangeScanner interface {
	Scan(dest ...any) error
}

func scanFileChange(scanner fileChangeScanner) (FileChange, error) {
	var c FileChange
	var prev sql.NullString
	if err := scanner.Scan(&c.ID, &c.DirectoryPath, &c.FilePath, &c.AbsolutePath, &c.SHA, &prev, &c.Data, &c.ChangeType, &c.CreatedAt); err != nil {
		return FileChange{}, err
	}
	if prev.Valid {
		c.Previous = prev.String
	}
	if c.ChangeType == "" {
		c.ChangeType = ChangeTypeModify
	}
	return c, nil
}

func ensureChangesColumn(name, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(changes)`)
	if err != nil {
		return fmt.Errorf("inspect changes table: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var columnName string
		var columnType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan changes schema: %w", err)
		}
		if columnName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("changes schema iteration: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE changes ADD COLUMN ` + definition); err != nil {
		return fmt.Errorf("add changes.%s column: %w", name, err)
	}
	return nil
}

func getLatestFileChange(directoryPath, filePath string) (FileChange, bool, error) {
	row := db.QueryRow(
		`SELECT id, directory_path, file_path, absolute_path, sha, previous, data, change_type, created_at
		 FROM changes
		 WHERE directory_path = ? AND file_path = ?
		 ORDER BY id DESC
		 LIMIT 1`, directoryPath, filePath)

	change, err := scanFileChange(row)
	if err == sql.ErrNoRows {
		return FileChange{}, false, nil
	}
	if err != nil {
		return FileChange{}, false, fmt.Errorf("lookup latest change: %w", err)
	}
	return change, true, nil
}

func deletionSHA(filePath, previousSHA string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("delete\x00"+filePath+"\x00"+previousSHA)))
}
