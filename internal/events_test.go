package internal

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestWatchInvalidFlag(t *testing.T) {
	var buf bytes.Buffer
	err := Watch([]string{"--bogus"}, &buf)
	if err == nil {
		t.Fatal("expected error for invalid flag")
	}
}

func TestWatchHelpFlag(t *testing.T) {
	var buf bytes.Buffer
	err := Watch([]string{"-h"}, &buf)
	if err == nil {
		t.Fatal("expected error for help flag")
	}
}

func TestAddDirsRecursive(t *testing.T) {
	dir := t.TempDir()

	sub1 := filepath.Join(dir, "sub1")
	sub2 := filepath.Join(dir, "sub2")
	subsub := filepath.Join(sub1, "subsub")
	for _, d := range []string{sub1, sub2, subsub} {
		if err := os.Mkdir(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	if err := addDirsRecursive(watcher, dir); err != nil {
		t.Fatalf("addDirsRecursive failed: %v", err)
	}

	watched := watcher.WatchList()
	slices.Sort(watched)

	expected := []string{dir, sub1, sub2, subsub}
	for i, p := range expected {
		abs, _ := filepath.Abs(p)
		expected[i] = abs
	}
	slices.Sort(expected)

	if len(watched) != len(expected) {
		t.Fatalf("expected %d watched dirs, got %d: %v", len(expected), len(watched), watched)
	}
	for i := range expected {
		if watched[i] != expected[i] {
			t.Errorf("watched[%d] = %q, want %q", i, watched[i], expected[i])
		}
	}
}

func TestAddDirsRecursiveSkipsHidden(t *testing.T) {
	dir := t.TempDir()

	visible := filepath.Join(dir, "visible")
	hidden := filepath.Join(dir, ".hidden")
	nested := filepath.Join(hidden, "nested")
	for _, d := range []string{visible, hidden, nested} {
		if err := os.Mkdir(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	if err := addDirsRecursive(watcher, dir); err != nil {
		t.Fatalf("addDirsRecursive failed: %v", err)
	}

	watched := watcher.WatchList()
	slices.Sort(watched)

	absVisible, _ := filepath.Abs(visible)
	absHidden, _ := filepath.Abs(hidden)
	absNested, _ := filepath.Abs(nested)
	absDir, _ := filepath.Abs(dir)

	for _, p := range watched {
		if p == absHidden || p == absNested {
			t.Errorf("should not watch hidden dir: %s", p)
		}
	}

	foundVisible := false
	foundRoot := false
	for _, p := range watched {
		if p == absVisible {
			foundVisible = true
		}
		if p == absDir {
			foundRoot = true
		}
	}
	if !foundVisible {
		t.Error("should watch visible subdirectory")
	}
	if !foundRoot {
		t.Error("should watch root directory")
	}
}

func TestAddDirsRecursiveNonExistent(t *testing.T) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	err = addDirsRecursive(watcher, "/nonexistent/path/should/not/exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(watcher.WatchList()) != 0 {
		t.Error("expected no watched dirs for non-existent path")
	}
}

func TestAddDirsRecursiveEmpty(t *testing.T) {
	dir := t.TempDir()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	if err := addDirsRecursive(watcher, dir); err != nil {
		t.Fatalf("addDirsRecursive failed: %v", err)
	}

	watched := watcher.WatchList()
	absDir, _ := filepath.Abs(dir)

	if len(watched) != 1 || watched[0] != absDir {
		t.Errorf("expected to watch only root, got %v", watched)
	}
}

func TestGitignoreToRegex(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		{"*.log", "app.log", true},
		{"*.log", "dir/app.log", true},
		{"*.log", "dir/sub/app.log", true},
		{"*.log", "app.log.txt", false},

		{"build/", "build", true},
		{"build/", "build/output.txt", true},
		{"build/", "src/build", true},

		{"node_modules/", "node_modules/pkg/index.js", true},
		{"node_modules/", "foo/node_modules", true},

		{"/dist/*.js", "dist/bundle.js", true},
		{"/dist/*.js", "app/dist/bundle.js", false},
		{"/dist/*.js", "dist/sub/bundle.js", false},

		{"**/*.swp", "foo/bar.swp", true},
		{"**/*.swp", "a/b/c/d.swp", true},
		{"**/*.swp", "x.swp", true},

		{"doc/*.txt", "doc/readme.txt", true},
		{"doc/*.txt", "a/doc/readme.txt", true},
		{"doc/*.txt", "adoc/readme.txt", false},

		{"temp", "temp", true},
		{"temp", "dir/temp", true},
		{"temp", "temp/foo", false},

		{"?at", "cat", true},
		{"?at", "bat", true},
		{"?at", "at", false},
		{"?at", "dir/bat", true},

		{"", "anything", false},
		{"# comment", "anything", false},
	}

	for _, tt := range tests {
		re := gitignoreToRegex(tt.pattern)
		got := re != nil && re.MatchString(tt.path)
		if got != tt.match {
			t.Errorf("gitignoreToRegex(%q).MatchString(%q) = %v, want %v", tt.pattern, tt.path, got, tt.match)
		}
	}
}

func TestLoadGitignore(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file returns nil", func(t *testing.T) {
		saved := gitignorePatterns
		gitignorePatterns = nil
		defer func() { gitignorePatterns = saved }()

		err := loadGitignore(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("parses patterns", func(t *testing.T) {
		saved := gitignorePatterns
		gitignorePatterns = nil
		defer func() { gitignorePatterns = saved }()

		content := "*.log\nbuild/\n# comment\n\n!important.log\n"
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		if err := loadGitignore(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(gitignorePatterns) != 2 {
			t.Fatalf("expected 2 patterns, got %d", len(gitignorePatterns))
		}

		if !gitignorePatterns[1].MatchString("build") {
			t.Errorf("build/ should match 'build': %s", gitignorePatterns[1])
		}
		if !gitignorePatterns[1].MatchString("sub/build") {
			t.Errorf("build/ should match 'sub/build': %s", gitignorePatterns[1])
		}
	})
}

func TestShouldIgnoreWithGitignore(t *testing.T) {
	saved := gitignorePatterns
	gitignorePatterns = nil
	defer func() { gitignorePatterns = saved }()

	gitignorePatterns = []*regexp.Regexp{
		gitignoreToRegex("*.tmp"),
		gitignoreToRegex("dist/"),
		gitignoreToRegex("secret.key"),
	}

	tests := []struct {
		path   string
		ignore bool
	}{
		{"./file.tmp", true},
		{"file.tmp", true},
		{"dir/file.tmp", true},
		{"./dist/main.js", true},
		{"dist/main.js", true},
		{"src/secret.key", true},
		{"./src/secret.key", true},
		{"./app/main.go", false},
		{"app/main.go", false},
		{"readme.md", false},
	}

	for _, tt := range tests {
		got := shouldIgnore(tt.path)
		if got != tt.ignore {
			t.Errorf("shouldIgnore(%q) = %v, want %v", tt.path, got, tt.ignore)
		}
	}
}

func TestAddDirsRecursiveSkipsGitignore(t *testing.T) {
	saved := gitignorePatterns
	gitignorePatterns = nil
	defer func() { gitignorePatterns = saved }()

	gitignorePatterns = []*regexp.Regexp{
		gitignoreToRegex("dist/"),
		gitignoreToRegex("cache/"),
	}

	dir := t.TempDir()

	keep := filepath.Join(dir, "src")
	skip1 := filepath.Join(dir, "dist")
	skip2 := filepath.Join(dir, "cache")
	skipSub := filepath.Join(skip1, "sub")
	for _, d := range []string{keep, skip1, skip2, skipSub} {
		if err := os.Mkdir(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	if err := addDirsRecursive(watcher, dir); err != nil {
		t.Fatalf("addDirsRecursive failed: %v", err)
	}

	watched := watcher.WatchList()
	for _, p := range watched {
		absSkip1, _ := filepath.Abs(skip1)
		absSkip2, _ := filepath.Abs(skip2)
		absSkipSub, _ := filepath.Abs(skipSub)
		if p == absSkip1 || p == absSkip2 || p == absSkipSub {
			t.Errorf("should not watch gitignored dir: %s", p)
		}
	}

	found := false
	absKeep, _ := filepath.Abs(keep)
	for _, p := range watched {
		if p == absKeep {
			found = true
		}
	}
	if !found {
		t.Error("should watch non-ignored src dir")
	}
}
