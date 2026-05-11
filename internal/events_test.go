package internal

import (
	"bytes"
	"os"
	"path/filepath"
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
