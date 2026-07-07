package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChronicleDirDefaultsToHomeDotConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(configHomeEnv, "")

	dir, err := ChronicleDir()
	if err != nil {
		t.Fatalf("ChronicleDir failed: %v", err)
	}

	expected := filepath.Join(home, ".config", "chronicle")
	if dir != expected {
		t.Fatalf("expected %q, got %q", expected, dir)
	}
}

func TestLoadConfigCreatesTOMLFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv(configHomeEnv, root)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Directories) != 0 {
		t.Fatalf("expected empty directories, got %v", cfg.Directories)
	}

	data, err := os.ReadFile(filepath.Join(root, configName))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "directories = [") {
		t.Fatalf("config should be TOML array, got:\n%s", data)
	}
}

func TestAddAndRemoveMonitoredDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv(configHomeEnv, root)
	monitored := filepath.Join(root, "project")
	if err := os.Mkdir(monitored, 0755); err != nil {
		t.Fatal(err)
	}

	cfg, err := AddMonitoredDirectory(monitored)
	if err != nil {
		t.Fatalf("AddMonitoredDirectory failed: %v", err)
	}
	if len(cfg.Directories) != 1 || cfg.Directories[0] != monitored {
		t.Fatalf("unexpected directories: %v", cfg.Directories)
	}

	cfg, err = AddMonitoredDirectory(monitored)
	if err != nil {
		t.Fatalf("duplicate AddMonitoredDirectory failed: %v", err)
	}
	if len(cfg.Directories) != 1 {
		t.Fatalf("duplicate add should not append: %v", cfg.Directories)
	}

	cfg, err = RemoveMonitoredDirectory(monitored)
	if err != nil {
		t.Fatalf("RemoveMonitoredDirectory failed: %v", err)
	}
	if len(cfg.Directories) != 0 {
		t.Fatalf("expected no directories, got %v", cfg.Directories)
	}
}

func TestParseConfigSupportsInlineAndMultilineArrays(t *testing.T) {
	inline, err := parseConfig([]byte(`directories = ["/a", "/b"]`))
	if err != nil {
		t.Fatalf("parse inline: %v", err)
	}
	if len(inline.Directories) != 2 || inline.Directories[0] != "/a" || inline.Directories[1] != "/b" {
		t.Fatalf("unexpected inline directories: %v", inline.Directories)
	}

	multiline, err := parseConfig([]byte(`
# local roots
directories = [
  "/a", # first
  "/b#literal",
]
`))
	if err != nil {
		t.Fatalf("parse multiline: %v", err)
	}
	if len(multiline.Directories) != 2 || multiline.Directories[1] != "/b#literal" {
		t.Fatalf("unexpected multiline directories: %v", multiline.Directories)
	}
}
