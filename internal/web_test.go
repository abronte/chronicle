package internal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func setupWebTest(t *testing.T) http.Handler {
	t.Helper()

	t.Setenv(configHomeEnv, t.TempDir())
	db = nil
	if err := InitializeCentralDB(); err != nil {
		t.Fatalf("InitializeCentralDB failed: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			db.Close()
			db = nil
		}
	})

	return NewWebHandler()
}

func TestWebCanAddListAndDeleteDirectories(t *testing.T) {
	handler := setupWebTest(t)
	monitored := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(monitored, 0755); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"path": {monitored}}
	req := httptest.NewRequest(http.MethodPost, "/directories", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect after add, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, monitored) {
		t.Fatalf("home page should list monitored directory, got:\n%s", body)
	}

	form = url.Values{"path": {monitored}}
	req = httptest.NewRequest(http.MethodPost, "/directories/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect after delete, got %d", rec.Code)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Directories) != 0 {
		t.Fatalf("expected directory to be deleted, got %v", cfg.Directories)
	}
}

func TestWebCanAddListAndDeleteIgnorePatterns(t *testing.T) {
	handler := setupWebTest(t)

	form := url.Values{"pattern": {"*.ndjson"}}
	req := httptest.NewRequest(http.MethodPost, "/ignore-patterns", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect after add, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Global Ignore Patterns") || !strings.Contains(body, "*.ndjson") {
		t.Fatalf("home page should list ignore pattern, got:\n%s", body)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.IgnorePatterns) != 1 || cfg.IgnorePatterns[0] != "*.ndjson" {
		t.Fatalf("expected ignore pattern in config, got %v", cfg.IgnorePatterns)
	}

	form = url.Values{"pattern": {"*.ndjson"}}
	req = httptest.NewRequest(http.MethodPost, "/ignore-patterns/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect after delete, got %d", rec.Code)
	}

	cfg, err = LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.IgnorePatterns) != 0 {
		t.Fatalf("expected ignore pattern to be deleted, got %v", cfg.IgnorePatterns)
	}
}

func TestWebShowsDirectoryHistoryAndFileDiff(t *testing.T) {
	handler := setupWebTest(t)
	root := t.TempDir()
	if err := SaveConfig(Config{Directories: []string{root}}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	addChangeWithDelayForDirectory(t, root, "app.go", "package main\n")
	addChangeWithDelayForDirectory(t, root, "app.go", "package main\n\nfunc main() {}\n")

	req := httptest.NewRequest(http.MethodGet, "/history?dir="+url.QueryEscape(root), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected history status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Change History") || !strings.Contains(body, "app.go") {
		t.Fatalf("history page missing expected content:\n%s", body)
	}

	fileURL := "/file?dir=" + url.QueryEscape(root) + "&file=" + url.QueryEscape("app.go")
	req = httptest.NewRequest(http.MethodGet, fileURL, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected file status 200, got %d", rec.Code)
	}
	bodyBytes, _ := io.ReadAll(rec.Body)
	body = string(bodyBytes)
	if !strings.Contains(body, "Diff Viewer") || !strings.Contains(body, `id="diff-viewer"`) {
		t.Fatalf("file page missing diff viewer container:\n%s", body)
	}
	if !strings.Contains(body, `https://esm.sh/@pierre/diffs@1.2.12?standalone`) {
		t.Fatalf("file page should import the Diffs browser module:\n%s", body)
	}
	if !strings.Contains(body, `diffStyle: "unified"`) {
		t.Fatalf("file page should default to the stacked/unified diff style:\n%s", body)
	}
	if !strings.Contains(body, `"oldFile":{"name":"app.go","contents":"package main\n"`) ||
		!strings.Contains(body, `"newFile":{"name":"app.go","contents":"package main\n\nfunc main() {}\n"`) {
		t.Fatalf("file page should expose old and new contents for browser diff rendering:\n%s", body)
	}
}

func TestWebCanShowFullFileForSelectedChange(t *testing.T) {
	handler := setupWebTest(t)
	root := t.TempDir()
	if err := SaveConfig(Config{Directories: []string{root}}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	addChangeWithDelayForDirectory(t, root, "app.go", "old version\n")
	addChangeWithDelayForDirectory(t, root, "app.go", "new version\nsecond line\n")

	changes, err := GetFileHistoryForDirectory(root, "app.go", 10)
	if err != nil {
		t.Fatalf("GetFileHistoryForDirectory failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	oldChangeID := strconv.FormatInt(changes[1].ID, 10)

	fileURL := "/file?dir=" + url.QueryEscape(root) + "&file=" + url.QueryEscape("app.go")
	req := httptest.NewRequest(http.MethodGet, fileURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Show Full") || !strings.Contains(body, "view=full") {
		t.Fatalf("diff view should include a Show Full button:\n%s", body)
	}

	fullURL := fileURL + "&change=" + oldChangeID + "&view=full"
	req = httptest.NewRequest(http.MethodGet, fullURL, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected full file status 200, got %d", rec.Code)
	}
	body = rec.Body.String()
	if !strings.Contains(body, "Full File") || !strings.Contains(body, "Show Diff") {
		t.Fatalf("full view should include a full-file header and return button:\n%s", body)
	}
	if !strings.Contains(body, `<span class="diff-code">old version</span>`) {
		t.Fatalf("full view should show the selected historical file content:\n%s", body)
	}
	if strings.Contains(body, "new version") || strings.Contains(body, "second line") {
		t.Fatalf("full view should not show content from a newer version:\n%s", body)
	}
}

func TestWebShowsDeletedFileHistoryAndDiff(t *testing.T) {
	handler := setupWebTest(t)
	root := t.TempDir()
	if err := SaveConfig(Config{Directories: []string{root}}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	addChangeWithDelayForDirectory(t, root, "notes.txt", "hello\nworld\n")
	deleteSHA, err := AddDeleteForDirectory(root, "notes.txt")
	if err != nil {
		t.Fatalf("AddDeleteForDirectory failed: %v", err)
	}
	if deleteSHA == "" {
		t.Fatal("expected delete change")
	}

	req := httptest.NewRequest(http.MethodGet, "/history?dir="+url.QueryEscape(root), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected history status 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "notes.txt") || !strings.Contains(body, "Deleted") {
		t.Fatalf("history page should show deleted file:\n%s", body)
	}

	fileURL := "/file?dir=" + url.QueryEscape(root) + "&file=" + url.QueryEscape("notes.txt")
	req = httptest.NewRequest(http.MethodGet, fileURL, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected file status 200, got %d", rec.Code)
	}
	body = rec.Body.String()
	if !strings.Contains(body, "Deleted") {
		t.Fatalf("file page should label deleted version:\n%s", body)
	}
	if !strings.Contains(body, `"oldFile":{"name":"notes.txt","contents":"hello\nworld\n"`) ||
		!strings.Contains(body, `"newFile":{"name":"notes.txt","contents":""}`) {
		t.Fatalf("file page should expose deleted file contents as old file data:\n%s", body)
	}
}

func TestFileVersionAPIReturnsVersionToLoopbackRequest(t *testing.T) {
	handler := setupWebTest(t)
	root := t.TempDir()
	filePath := filepath.Join(root, "restore.txt")
	sha := addChangeWithDelayForDirectory(t, root, filePath, "saved version\n")

	query := url.Values{
		"path":    {filePath},
		"version": {sha[:12]},
	}
	req := httptest.NewRequest(http.MethodGet, FileVersionAPIPath+"?"+query.Encode(), nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected file-version status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(FileVersionAPIHeader); got != FileVersionAPIHeaderName {
		t.Fatalf("expected API marker header %q, got %q", FileVersionAPIHeaderName, got)
	}

	var response FileVersionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode file-version response: %v", err)
	}
	if response.AbsolutePath != filePath || response.SHA != sha || response.Data != "saved version\n" {
		t.Fatalf("unexpected file-version response: %#v", response)
	}
}

func TestFileVersionAPIRejectsNonLoopbackRequest(t *testing.T) {
	handler := setupWebTest(t)
	root := t.TempDir()
	filePath := filepath.Join(root, "private.txt")
	addChangeWithDelayForDirectory(t, root, filePath, "private history\n")

	req := httptest.NewRequest(http.MethodGet, FileVersionAPIPath+"?path="+url.QueryEscape(filePath), nil)
	req.RemoteAddr = "203.0.113.10:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected non-loopback request to be rejected with 404, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "private history") {
		t.Fatalf("non-loopback response exposed file contents: %q", rec.Body.String())
	}
}

func TestFormatDiffAddsLineNumbersAndMarkers(t *testing.T) {
	lines := formatDiff("@@ -3,1 +3,2 @@\n-old\n+new\n+extra")
	if len(lines) != 4 {
		t.Fatalf("expected 4 diff lines, got %d", len(lines))
	}

	if lines[1].OldLine != "3" || lines[1].NewLine != "" || lines[1].Marker != "-" || lines[1].Code != "old" {
		t.Fatalf("unexpected delete line metadata: %#v", lines[1])
	}
	if lines[2].OldLine != "" || lines[2].NewLine != "3" || lines[2].Marker != "+" || lines[2].Code != "new" {
		t.Fatalf("unexpected add line metadata: %#v", lines[2])
	}
	if lines[3].OldLine != "" || lines[3].NewLine != "4" || lines[3].Marker != "+" || lines[3].Code != "extra" {
		t.Fatalf("unexpected second add line metadata: %#v", lines[3])
	}
}
