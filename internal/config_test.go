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
	if !strings.Contains(string(data), "ignore_patterns = [") {
		t.Fatalf("config should include ignore pattern array, got:\n%s", data)
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

func TestAddAndRemoveIgnorePattern(t *testing.T) {
	root := t.TempDir()
	t.Setenv(configHomeEnv, root)

	cfg, err := AddIgnorePattern("*.ndjson")
	if err != nil {
		t.Fatalf("AddIgnorePattern failed: %v", err)
	}
	if len(cfg.IgnorePatterns) != 1 || cfg.IgnorePatterns[0] != "*.ndjson" {
		t.Fatalf("unexpected ignore patterns: %v", cfg.IgnorePatterns)
	}

	cfg, err = AddIgnorePattern("*.ndjson")
	if err != nil {
		t.Fatalf("duplicate AddIgnorePattern failed: %v", err)
	}
	if len(cfg.IgnorePatterns) != 1 {
		t.Fatalf("duplicate add should not append: %v", cfg.IgnorePatterns)
	}

	data, err := os.ReadFile(filepath.Join(root, configName))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"*.ndjson"`) {
		t.Fatalf("config should persist ignore pattern, got:\n%s", data)
	}

	cfg, err = RemoveIgnorePattern("*.ndjson")
	if err != nil {
		t.Fatalf("RemoveIgnorePattern failed: %v", err)
	}
	if len(cfg.IgnorePatterns) != 0 {
		t.Fatalf("expected no ignore patterns, got %v", cfg.IgnorePatterns)
	}
}

func TestParseConfigSupportsInlineAndMultilineArrays(t *testing.T) {
	inline, err := parseConfig([]byte(`
directories = ["/a", "/b"]
ignore_patterns = ["*.ndjson", "dist/"]
`))
	if err != nil {
		t.Fatalf("parse inline: %v", err)
	}
	if len(inline.Directories) != 2 || inline.Directories[0] != "/a" || inline.Directories[1] != "/b" {
		t.Fatalf("unexpected inline directories: %v", inline.Directories)
	}
	if len(inline.IgnorePatterns) != 2 || inline.IgnorePatterns[0] != "*.ndjson" || inline.IgnorePatterns[1] != "dist/" {
		t.Fatalf("unexpected inline ignore patterns: %v", inline.IgnorePatterns)
	}

	multiline, err := parseConfig([]byte(`
# local roots
directories = [
  "/a", # first
  "/b#literal",
]
ignore_patterns = [
  "*.ndjson",
  "tmp/",
]
`))
	if err != nil {
		t.Fatalf("parse multiline: %v", err)
	}
	if len(multiline.Directories) != 2 || multiline.Directories[1] != "/b#literal" {
		t.Fatalf("unexpected multiline directories: %v", multiline.Directories)
	}
	if len(multiline.IgnorePatterns) != 2 || multiline.IgnorePatterns[1] != "tmp/" {
		t.Fatalf("unexpected multiline ignore patterns: %v", multiline.IgnorePatterns)
	}
}
