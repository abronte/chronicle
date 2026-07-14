package internal

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	db = nil
	dir := t.TempDir()
	if err := InitializeDB(dir); err != nil {
		t.Fatalf("failed to initialize test db: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			db.Close()
			db = nil
		}
	})
}

func addChangeWithDelay(t *testing.T, filePath, data string) string {
	t.Helper()
	sha, err := AddChange(filePath, data)
	if err != nil {
		t.Fatalf("AddChange failed: %v", err)
	}
	time.Sleep(time.Millisecond * 2)
	return sha
}

func TestInitializeDB(t *testing.T) {
	db = nil
	t.Cleanup(func() {
		if db != nil {
			db.Close()
			db = nil
		}
	})

	dir := t.TempDir()
	if err := InitializeDB(dir); err != nil {
		t.Fatalf("InitializeDB failed: %v", err)
	}
	if db == nil {
		t.Fatal("db should not be nil after init")
	}

	if err := InitializeDB(dir); err != nil {
		t.Fatalf("second InitializeDB failed: %v", err)
	}
}

func TestInitializeDBAtMigratesLegacyChangesTable(t *testing.T) {
	db = nil
	t.Cleanup(func() {
		if db != nil {
			db.Close()
			db = nil
		}
	})

	dbPath := filepath.Join(t.TempDir(), historyName)
	legacy, err := sql.Open("turso", localTursoDSN(dbPath))
	if err != nil {
		t.Fatalf("open legacy db failed: %v", err)
	}
	_, err = legacy.Exec(`CREATE TABLE changes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		directory_path TEXT NOT NULL,
		file_path TEXT NOT NULL,
		absolute_path TEXT NOT NULL,
		sha TEXT NOT NULL,
		previous TEXT,
		data TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create legacy table failed: %v", err)
	}
	_, err = legacy.Exec(
		`INSERT INTO changes (directory_path, file_path, absolute_path, sha, previous, data, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		".", "legacy.txt", "legacy.txt", "abc123", nil, "old data", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("insert legacy row failed: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db failed: %v", err)
	}

	if err := InitializeDBAt(dbPath); err != nil {
		t.Fatalf("InitializeDBAt failed: %v", err)
	}

	changes, err := GetFileHistory("legacy.txt", 10)
	if err != nil {
		t.Fatalf("GetFileHistory failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 legacy change, got %d", len(changes))
	}
	if changes[0].ChangeType != ChangeTypeModify {
		t.Fatalf("expected legacy change type %q, got %q", ChangeTypeModify, changes[0].ChangeType)
	}
}

func TestAddChange(t *testing.T) {
	setupTestDB(t)

	sha, err := AddChange("test/file.txt", "hello world")
	if err != nil {
		t.Fatalf("AddChange failed: %v", err)
	}
	if sha == "" {
		t.Error("expected non-empty sha")
	}

	sha2, err := AddChange("test/file.txt", "hello world")
	if err != nil {
		t.Fatalf("AddChange failed: %v", err)
	}
	if sha2 != "" {
		t.Error("expected empty sha (duplicate content)")
	}

	sha3, err := AddChange("test/file.txt", "hello world updated")
	if err != nil {
		t.Fatalf("AddChange failed: %v", err)
	}
	if sha3 == "" {
		t.Error("expected non-empty sha for different content")
	}
}

func TestAddDeleteForDirectoryRecordsDeleteAndRetainsData(t *testing.T) {
	setupTestDB(t)

	root := t.TempDir()
	fileSHA := addChangeWithDelayForDirectory(t, root, "note.txt", "hello\n")

	deleteSHA, err := AddDeleteForDirectory(root, "note.txt")
	if err != nil {
		t.Fatalf("AddDeleteForDirectory failed: %v", err)
	}
	if deleteSHA == "" {
		t.Fatal("expected delete sha")
	}
	if deleteSHA == fileSHA {
		t.Fatal("delete sha should be distinct from content sha")
	}

	changes, err := GetFileHistoryForDirectory(root, "note.txt", 10)
	if err != nil {
		t.Fatalf("GetFileHistoryForDirectory failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	if changes[0].ChangeType != ChangeTypeDelete {
		t.Fatalf("expected latest change type %q, got %q", ChangeTypeDelete, changes[0].ChangeType)
	}
	if changes[0].Previous != fileSHA {
		t.Fatalf("expected delete previous sha %q, got %q", fileSHA, changes[0].Previous)
	}
	if changes[0].Data != "hello\n" {
		t.Fatalf("expected delete row to retain previous data, got %q", changes[0].Data)
	}
	if changes[1].ChangeType != ChangeTypeModify {
		t.Fatalf("expected original change type %q, got %q", ChangeTypeModify, changes[1].ChangeType)
	}

	files, err := GetChangedFilesForDirectory(root, 10)
	if err != nil {
		t.Fatalf("GetChangedFilesForDirectory failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(files))
	}
	if files[0].ChangeType != ChangeTypeDelete {
		t.Fatalf("expected changed file type %q, got %q", ChangeTypeDelete, files[0].ChangeType)
	}

	duplicateSHA, err := AddDeleteForDirectory(root, "note.txt")
	if err != nil {
		t.Fatalf("duplicate AddDeleteForDirectory failed: %v", err)
	}
	if duplicateSHA != "" {
		t.Fatalf("expected duplicate delete to be skipped, got %q", duplicateSHA)
	}

	recreatedSHA, err := AddChangeForDirectory(root, "note.txt", "hello\n")
	if err != nil {
		t.Fatalf("AddChangeForDirectory after delete failed: %v", err)
	}
	if recreatedSHA == "" {
		t.Fatal("expected recreated file with same content to be recorded")
	}
}

func TestAddDeleteForDirectorySkipsUnknownFile(t *testing.T) {
	setupTestDB(t)

	root := t.TempDir()
	sha, err := AddDeleteForDirectory(root, "unknown.txt")
	if err != nil {
		t.Fatalf("AddDeleteForDirectory failed: %v", err)
	}
	if sha != "" {
		t.Fatalf("expected unknown delete to be skipped, got %q", sha)
	}

	changes, err := GetDirectoryChanges(root, 10)
	if err != nil {
		t.Fatalf("GetDirectoryChanges failed: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes, got %d", len(changes))
	}
}

func TestGetRecentChanges(t *testing.T) {
	setupTestDB(t)

	addChangeWithDelay(t, "file1.txt", "content1")
	addChangeWithDelay(t, "file2.txt", "content2")

	changes, err := GetRecentChanges(10)
	if err != nil {
		t.Fatalf("GetRecentChanges failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 recent changes, got %d", len(changes))
	}

	paths := map[string]bool{}
	for _, c := range changes {
		paths[c.FilePath] = true
		if c.AbsolutePath == "" {
			t.Error("expected absolute path in recent change")
		}
	}
	if !paths["file1.txt"] || !paths["file2.txt"] {
		t.Error("expected both file paths in results")
	}
}

func TestGetRecentChangesLimit(t *testing.T) {
	setupTestDB(t)

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		addChangeWithDelay(t, name+".txt", "content")
	}

	changes, err := GetRecentChanges(3)
	if err != nil {
		t.Fatalf("GetRecentChanges failed: %v", err)
	}
	if len(changes) != 3 {
		t.Errorf("expected 3 changes with limit, got %d", len(changes))
	}
}

func TestGetFileHistory(t *testing.T) {
	setupTestDB(t)

	addChangeWithDelay(t, "history.txt", "version 1")
	addChangeWithDelay(t, "history.txt", "version 2")
	addChangeWithDelay(t, "history.txt", "version 3")

	changes, err := GetFileHistory("history.txt", 10)
	if err != nil {
		t.Fatalf("GetFileHistory failed: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(changes))
	}

	if changes[0].Data != "version 3" {
		t.Errorf("expected newest first, got %q", changes[0].Data)
	}
	if changes[2].Data != "version 1" {
		t.Errorf("expected oldest last, got %q", changes[2].Data)
	}
}

func TestGetFileHistoryWithPrevious(t *testing.T) {
	setupTestDB(t)

	sha1 := addChangeWithDelay(t, "prev.txt", "first")
	sha2 := addChangeWithDelay(t, "prev.txt", "second")

	changes, err := GetFileHistory("prev.txt", 10)
	if err != nil {
		t.Fatalf("GetFileHistory failed: %v", err)
	}
	if len(changes) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(changes))
	}

	if changes[1].SHA != sha1 {
		t.Errorf("expected sha %s, got %s", sha1, changes[1].SHA)
	}
	if changes[1].Previous != "" {
		t.Errorf("expected empty previous for first entry, got %q", changes[1].Previous)
	}
	if changes[0].SHA != sha2 {
		t.Errorf("expected sha %s, got %s", sha2, changes[0].SHA)
	}
	if changes[0].Previous != sha1 {
		t.Errorf("expected previous %s, got %s", sha1, changes[0].Previous)
	}
}

func TestGetFileHistoryEmpty(t *testing.T) {
	setupTestDB(t)

	changes, err := GetFileHistory("nonexistent.txt", 10)
	if err != nil {
		t.Fatalf("GetFileHistory failed: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected empty result, got %d entries", len(changes))
	}
}

func TestGetFileVersionReturnsLatestDeleteWithRetainedContent(t *testing.T) {
	setupTestDB(t)

	root := t.TempDir()
	filePath := filepath.Join(root, "deleted.txt")
	addChangeWithDelayForDirectory(t, root, filePath, "restorable contents\n")
	deleteSHA, err := AddDeleteForDirectory(root, filePath)
	if err != nil {
		t.Fatalf("AddDeleteForDirectory failed: %v", err)
	}

	change, err := GetFileVersion(filePath, "")
	if err != nil {
		t.Fatalf("GetFileVersion failed: %v", err)
	}
	if change.SHA != deleteSHA {
		t.Fatalf("expected latest delete sha %q, got %q", deleteSHA, change.SHA)
	}
	if change.ChangeType != ChangeTypeDelete {
		t.Fatalf("expected change type %q, got %q", ChangeTypeDelete, change.ChangeType)
	}
	if change.Data != "restorable contents\n" {
		t.Fatalf("expected retained contents, got %q", change.Data)
	}
	if change.AbsolutePath != filePath {
		t.Fatalf("expected absolute path %q, got %q", filePath, change.AbsolutePath)
	}
}

func TestGetFileVersionSelectsFullAndShortSHA(t *testing.T) {
	setupTestDB(t)

	root := t.TempDir()
	filePath := filepath.Join(root, "versioned.txt")
	firstSHA := addChangeWithDelayForDirectory(t, root, filePath, "first version")
	secondSHA := addChangeWithDelayForDirectory(t, root, filePath, "second version")

	first, err := GetFileVersion(filePath, firstSHA)
	if err != nil {
		t.Fatalf("GetFileVersion with full SHA failed: %v", err)
	}
	if first.SHA != firstSHA || first.Data != "first version" {
		t.Fatalf("expected first version %q, got sha %q and data %q", firstSHA, first.SHA, first.Data)
	}

	second, err := GetFileVersion(filePath, strings.ToUpper(secondSHA[:12]))
	if err != nil {
		t.Fatalf("GetFileVersion with short SHA failed: %v", err)
	}
	if second.SHA != secondSHA || second.Data != "second version" {
		t.Fatalf("expected second version %q, got sha %q and data %q", secondSHA, second.SHA, second.Data)
	}
}

func TestGetFileVersionScopesLookupToExactAbsolutePath(t *testing.T) {
	setupTestDB(t)

	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	firstPath := filepath.Join(firstRoot, "src", "shared.go")
	secondPath := filepath.Join(secondRoot, "src", "shared.go")
	firstSHA := addChangeWithDelayForDirectory(t, firstRoot, firstPath, "first root")
	secondSHA := addChangeWithDelayForDirectory(t, secondRoot, secondPath, "second root")

	first, err := GetFileVersion(firstPath, "")
	if err != nil {
		t.Fatalf("GetFileVersion for first root failed: %v", err)
	}
	if first.AbsolutePath != firstPath || first.SHA != firstSHA || first.Data != "first root" {
		t.Fatalf("unexpected first-root version: %#v", first)
	}

	second, err := GetFileVersion(secondPath, "")
	if err != nil {
		t.Fatalf("GetFileVersion for second root failed: %v", err)
	}
	if second.AbsolutePath != secondPath || second.SHA != secondSHA || second.Data != "second root" {
		t.Fatalf("unexpected second-root version: %#v", second)
	}

	if _, err := GetFileVersion(firstPath, secondSHA); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected second-root SHA to be missing for first path, got %v", err)
	}
}

func TestGetFileVersionRejectsAmbiguousInvalidAndMissingVersions(t *testing.T) {
	setupTestDB(t)

	root := t.TempDir()
	filePath := filepath.Join(root, "ambiguous.txt")
	firstSHA := addChangeWithDelayForDirectory(t, root, filePath, "two")
	secondSHA := addChangeWithDelayForDirectory(t, root, filePath, "seven")
	if firstSHA[:1] != secondSHA[:1] {
		t.Fatalf("test fixture SHAs must share a prefix: %q and %q", firstSHA, secondSHA)
	}

	if _, err := GetFileVersion(filePath, firstSHA[:1]); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous prefix error, got %v", err)
	}
	if _, err := GetFileVersion(filePath, "not-a-sha"); err == nil || !strings.Contains(err.Error(), "SHA or SHA prefix") {
		t.Fatalf("expected invalid version error, got %v", err)
	}
	if _, err := GetFileVersion(filePath, strings.Repeat("f", 64)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing version error, got %v", err)
	}
	if _, err := GetFileVersion(filepath.Join(root, "missing.txt"), ""); err == nil || !strings.Contains(err.Error(), "no changes recorded") {
		t.Fatalf("expected missing file history error, got %v", err)
	}
}

func TestGetRecentChangesEmpty(t *testing.T) {
	setupTestDB(t)

	changes, err := GetRecentChanges(10)
	if err != nil {
		t.Fatalf("GetRecentChanges failed: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected empty result, got %d entries", len(changes))
	}
}

func TestDBFunctionsWithoutInit(t *testing.T) {
	db = nil
	t.Cleanup(func() { db = nil })

	_, err := GetRecentChanges(10)
	if err == nil {
		t.Error("expected error for uninitialized db")
	}

	_, err = GetFileHistory("test.txt", 10)
	if err == nil {
		t.Error("expected error for uninitialized db")
	}

	_, err = GetFileVersion("test.txt", "")
	if err == nil {
		t.Error("expected GetFileVersion error for uninitialized db")
	}
}

func TestAddChangeForDirectoryStoresRootAndRelativePath(t *testing.T) {
	setupTestDB(t)

	root := t.TempDir()
	filePath := filepath.Join(root, "src", "app.go")
	sha, err := AddChangeForDirectory(root, filePath, "package main\n")
	if err != nil {
		t.Fatalf("AddChangeForDirectory failed: %v", err)
	}
	if sha == "" {
		t.Fatal("expected sha")
	}

	changes, err := GetDirectoryChanges(root, 10)
	if err != nil {
		t.Fatalf("GetDirectoryChanges failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].DirectoryPath != root {
		t.Errorf("expected directory %q, got %q", root, changes[0].DirectoryPath)
	}
	if changes[0].FilePath != "src/app.go" {
		t.Errorf("expected relative path src/app.go, got %q", changes[0].FilePath)
	}
	if changes[0].AbsolutePath != filePath {
		t.Errorf("expected absolute path %q, got %q", filePath, changes[0].AbsolutePath)
	}
}

func TestGetChangedFilesForDirectoryUsesLatestPerFile(t *testing.T) {
	setupTestDB(t)

	root := t.TempDir()
	addChangeWithDelayForDirectory(t, root, "a.txt", "one")
	addChangeWithDelayForDirectory(t, root, "b.txt", "two")
	addChangeWithDelayForDirectory(t, root, "a.txt", "three")

	files, err := GetChangedFilesForDirectory(root, 10)
	if err != nil {
		t.Fatalf("GetChangedFilesForDirectory failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].FilePath != "a.txt" {
		t.Fatalf("expected latest changed file a.txt first, got %q", files[0].FilePath)
	}
}

func TestGetPreviousChange(t *testing.T) {
	setupTestDB(t)

	root := t.TempDir()
	firstSHA := addChangeWithDelayForDirectory(t, root, "history.txt", "first")
	addChangeWithDelayForDirectory(t, root, "history.txt", "second")

	changes, err := GetFileHistoryForDirectory(root, "history.txt", 10)
	if err != nil {
		t.Fatalf("GetFileHistoryForDirectory failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}

	previous, ok, err := GetPreviousChange(changes[0])
	if err != nil {
		t.Fatalf("GetPreviousChange failed: %v", err)
	}
	if !ok {
		t.Fatal("expected previous change")
	}
	if previous.SHA != firstSHA {
		t.Fatalf("expected previous sha %s, got %s", firstSHA, previous.SHA)
	}
}

func addChangeWithDelayForDirectory(t *testing.T, root, filePath, data string) string {
	t.Helper()
	sha, err := AddChangeForDirectory(root, filePath, data)
	if err != nil {
		t.Fatalf("AddChangeForDirectory failed: %v", err)
	}
	time.Sleep(time.Millisecond * 2)
	return sha
}
