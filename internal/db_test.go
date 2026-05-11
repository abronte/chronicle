package internal

import (
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
}
