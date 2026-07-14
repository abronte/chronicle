package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"chronicle/internal"
)

func TestPrintHelp(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf)

	out := buf.String()
	if !strings.Contains(out, "Chronicle") {
		t.Error("help output should contain 'Chronicle'")
	}
	if !strings.Contains(out, "watch") {
		t.Error("help should mention watch command")
	}
	if !strings.Contains(out, "recent") {
		t.Error("help should mention recent command")
	}
	if !strings.Contains(out, "diffs") {
		t.Error("help should mention diffs command")
	}
	if !strings.Contains(out, "restore") {
		t.Error("help should mention restore command")
	}
	if !strings.Contains(out, "update") {
		t.Error("help should mention update command")
	}
}

func TestRunHelpCommand(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"help"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "Chronicle") {
		t.Error("expected help output")
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"nonexistent"}, &buf)
	if err == nil {
		t.Error("expected error for unknown command")
	}
	if !strings.Contains(buf.String(), "Chronicle") {
		t.Error("expected help output for unknown command")
	}
}

func TestRunWatchVersionShortFlag(t *testing.T) {
	var buf bytes.Buffer
	err := internal.Watch([]string{"-v"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := fmt.Sprintf("chronicle %s", internal.Version)
	if !strings.Contains(buf.String(), expected) {
		t.Errorf("expected version output, got: %s", buf.String())
	}
}

func TestRunWatchVersionLongFlag(t *testing.T) {
	var buf bytes.Buffer
	err := internal.Watch([]string{"-version"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := fmt.Sprintf("chronicle %s", internal.Version)
	if !strings.Contains(buf.String(), expected) {
		t.Errorf("expected version output, got: %s", buf.String())
	}
}

func TestRunWatchVersionViaRun(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"watch", "-v"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := fmt.Sprintf("chronicle %s", internal.Version)
	if !strings.Contains(buf.String(), expected) {
		t.Errorf("expected version output, got: %s", buf.String())
	}
}

func TestRunDiffsNoFile(t *testing.T) {
	var buf bytes.Buffer
	err := runDiffs([]string{}, &buf)
	if err == nil {
		t.Error("expected error for missing file path")
	}
	if !strings.Contains(err.Error(), "file path required") {
		t.Errorf("expected 'file path required' error, got: %v", err)
	}
}

func TestRunRestoreNoFile(t *testing.T) {
	var buf bytes.Buffer
	err := runRestore(nil, &buf)
	if err == nil {
		t.Fatal("expected error for missing file path")
	}
	if !strings.Contains(err.Error(), "file path required") {
		t.Fatalf("expected 'file path required' error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage: chronicle restore") {
		t.Fatalf("expected restore usage, got:\n%s", buf.String())
	}
}

func TestFileVersionURL(t *testing.T) {
	t.Run("defaults to the local Chronicle server and latest version", func(t *testing.T) {
		got, err := fileVersionURL("", "/tmp/a file.go", "")
		if err != nil {
			t.Fatalf("fileVersionURL returned an error: %v", err)
		}

		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatalf("parse generated URL: %v", err)
		}
		if parsed.Scheme != "http" || parsed.Host != "127.0.0.1:12345" {
			t.Fatalf("expected default local server, got %q", got)
		}
		if parsed.Path != internal.FileVersionAPIPath {
			t.Fatalf("expected path %q, got %q", internal.FileVersionAPIPath, parsed.Path)
		}
		if parsed.Query().Get("path") != "/tmp/a file.go" {
			t.Fatalf("expected file path query, got %q", parsed.Query().Get("path"))
		}
		if _, ok := parsed.Query()["version"]; ok {
			t.Fatalf("latest-version request should omit version query, got %q", got)
		}
	})

	t.Run("includes a requested version and replaces an address path", func(t *testing.T) {
		got, err := fileVersionURL("localhost:4567/old?ignored=yes#fragment", "/tmp/file.go", "abc123")
		if err != nil {
			t.Fatalf("fileVersionURL returned an error: %v", err)
		}

		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatalf("parse generated URL: %v", err)
		}
		if parsed.Path != internal.FileVersionAPIPath {
			t.Fatalf("expected path %q, got %q", internal.FileVersionAPIPath, parsed.Path)
		}
		if parsed.Query().Get("path") != "/tmp/file.go" || parsed.Query().Get("version") != "abc123" {
			t.Fatalf("unexpected query in %q", got)
		}
		if parsed.Fragment != "" || parsed.Query().Has("ignored") {
			t.Fatalf("generated URL retained address path data: %q", got)
		}
	})

	for _, addr := range []string{"https://127.0.0.1:12345", "http://example.com:12345"} {
		t.Run("rejects non-local HTTP address "+addr, func(t *testing.T) {
			if _, err := fileVersionURL(addr, "/tmp/file.go", ""); err == nil {
				t.Fatalf("expected %q to be rejected", addr)
			}
		})
	}
}

func TestGetFileVersionFromService(t *testing.T) {
	want := internal.FileVersionResponse{
		AbsolutePath: "/tmp/a file.go",
		SHA:          "abc123",
		Data:         "restored contents\n",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != internal.FileVersionAPIPath {
			t.Errorf("expected path %q, got %q", internal.FileVersionAPIPath, r.URL.Path)
		}
		if got := r.URL.Query().Get("path"); got != want.AbsolutePath {
			t.Errorf("expected file path %q, got %q", want.AbsolutePath, got)
		}
		if got := r.URL.Query().Get("version"); got != want.SHA {
			t.Errorf("expected version %q, got %q", want.SHA, got)
		}
		w.Header().Set(internal.FileVersionAPIHeader, internal.FileVersionAPIHeaderName)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(want); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	got, err := getFileVersionFromService(server.URL, want.AbsolutePath, want.SHA)
	if err != nil {
		t.Fatalf("getFileVersionFromService returned an error: %v", err)
	}
	if got != want {
		t.Fatalf("unexpected snapshot: got %#v, want %#v", got, want)
	}
}

func TestWriteRestoredFileCreatesParentsAndOverwrites(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nested", "path", "restored.txt")
	if err := writeRestoredFile(target, "first version\n"); err != nil {
		t.Fatalf("write new restored file: %v", err)
	}
	assertFileContents(t, target, "first version\n")
	createdInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat new restored file: %v", err)
	}
	if got := createdInfo.Mode().Perm(); got != 0600 {
		t.Fatalf("new restored file permissions = %o, want 600", got)
	}

	if err := os.Chmod(target, 0640); err != nil {
		t.Fatalf("set original permissions: %v", err)
	}
	if err := writeRestoredFile(target, "second version\n"); err != nil {
		t.Fatalf("overwrite restored file: %v", err)
	}
	assertFileContents(t, target, "second version\n")

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat restored file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0640 {
		t.Fatalf("restored file permissions = %o, want 640", got)
	}

	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatalf("read restore directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(target) {
		t.Fatalf("temporary restore file was not cleaned up: %#v", entries)
	}
}

func TestRunRestoreUsesServiceWithoutOpeningDatabase(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "missing", "restored.go")
	configHome := filepath.Join(root, "chronicle-config-must-not-be-created")
	t.Setenv("CHRONICLE_CONFIG_HOME", configHome)

	const data = "package restored\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("path"); got != target {
			t.Errorf("requested path = %q, want %q", got, target)
		}
		if _, ok := r.URL.Query()["version"]; ok {
			t.Errorf("default restore should request the latest version: %q", r.URL.RawQuery)
		}
		w.Header().Set(internal.FileVersionAPIHeader, internal.FileVersionAPIHeaderName)
		_ = json.NewEncoder(w).Encode(internal.FileVersionResponse{
			AbsolutePath: target,
			SHA:          "0123456789abcdef",
			Data:         data,
		})
	}))
	defer server.Close()

	var stdout bytes.Buffer
	if err := runRestore([]string{"-addr", server.URL, target}, &stdout); err != nil {
		t.Fatalf("runRestore returned an error: %v", err)
	}
	assertFileContents(t, target, data)
	if !strings.Contains(stdout.String(), "version 01234567") {
		t.Fatalf("unexpected restore output: %q", stdout.String())
	}
	if _, err := os.Stat(configHome); !os.IsNotExist(err) {
		t.Fatalf("API-first restore touched database config at %s", configHome)
	}
}

func TestRunRestoreFallsBackToDatabaseWhenServiceIsUnavailable(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "restored.go")
	t.Setenv("CHRONICLE_CONFIG_HOME", filepath.Join(root, "chronicle-config"))

	if err := internal.InitializeCentralDB(); err != nil {
		t.Fatalf("initialize central database: %v", err)
	}
	sha, err := internal.AddChangeForDirectory(root, target, "package fromhistory\n")
	if err != nil {
		t.Fatalf("seed restore history: %v", err)
	}
	if err := os.WriteFile(target, []byte("package overwritten\n"), 0600); err != nil {
		t.Fatalf("write overwrite target: %v", err)
	}

	unsupportedServer := httptest.NewServer(http.NotFoundHandler())
	defer unsupportedServer.Close()

	var stdout bytes.Buffer
	if err := runRestore([]string{"-addr", unsupportedServer.URL, target}, &stdout); err != nil {
		t.Fatalf("runRestore returned an error: %v", err)
	}
	assertFileContents(t, target, "package fromhistory\n")
	if !strings.Contains(stdout.String(), "version "+sha[:8]) {
		t.Fatalf("unexpected restore output: %q", stdout.String())
	}
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if got := string(data); got != want {
		t.Fatalf("contents of %s = %q, want %q", path, got, want)
	}
}

func TestDiffForCLIShowsDeletedFile(t *testing.T) {
	older := internal.FileChange{Data: "hello\n"}
	newer := internal.FileChange{Data: "hello\n", ChangeType: internal.ChangeTypeDelete}

	diff, oldPath, newPath := diffForCLI("note.txt", older, newer)
	if oldPath != "note.txt" || newPath != "/dev/null" {
		t.Fatalf("expected delete paths, got %q -> %q", oldPath, newPath)
	}
	if !strings.Contains(diff, "-hello") {
		t.Fatalf("expected delete diff, got:\n%s", diff)
	}
}

func TestDiffForCLIShowsRecreatedFile(t *testing.T) {
	older := internal.FileChange{Data: "hello\n", ChangeType: internal.ChangeTypeDelete}
	newer := internal.FileChange{Data: "hello\n", ChangeType: internal.ChangeTypeModify}

	diff, oldPath, newPath := diffForCLI("note.txt", older, newer)
	if oldPath != "/dev/null" || newPath != "note.txt" {
		t.Fatalf("expected recreate paths, got %q -> %q", oldPath, newPath)
	}
	if !strings.Contains(diff, "+hello") {
		t.Fatalf("expected add diff, got:\n%s", diff)
	}
}

func TestRunDefaultIsWatch(t *testing.T) {
	var buf bytes.Buffer
	err := run([]string{"-v"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := fmt.Sprintf("chronicle %s", internal.Version)
	if !strings.Contains(buf.String(), expected) {
		t.Errorf("expected version output for default watch command, got: %s", buf.String())
	}
}
