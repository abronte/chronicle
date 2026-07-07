package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	configHomeEnv = "CHRONICLE_CONFIG_HOME"
	configName    = "config.toml"
	historyName   = "history.db"
)

type Config struct {
	Directories []string
}

func ChronicleDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv(configHomeEnv)); override != "" {
		return filepath.Clean(override), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "chronicle"), nil
}

func ConfigFilePath() (string, error) {
	dir, err := ChronicleDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configName), nil
}

func HistoryDBPath() (string, error) {
	dir, err := ChronicleDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, historyName), nil
}

func LoadConfig() (Config, error) {
	path, err := ConfigFilePath()
	if err != nil {
		return Config{}, err
	}
	return LoadConfigAt(path)
}

func LoadConfigAt(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Config{}
			if err := SaveConfigAt(path, cfg); err != nil {
				return Config{}, err
			}
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg, err := parseConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.Directories = uniqueDirectories(cfg.Directories)
	return cfg, nil
}

func SaveConfig(cfg Config) error {
	path, err := ConfigFilePath()
	if err != nil {
		return err
	}
	return SaveConfigAt(path, cfg)
}

func SaveConfigAt(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	cfg.Directories = uniqueDirectories(cfg.Directories)
	var sb strings.Builder
	sb.WriteString("# Chronicle system-wide configuration.\n")
	sb.WriteString("directories = [\n")
	for _, dir := range cfg.Directories {
		sb.WriteString("  ")
		sb.WriteString(strconv.Quote(dir))
		sb.WriteString(",\n")
	}
	sb.WriteString("]\n")

	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func AddMonitoredDirectory(path string) (Config, error) {
	dir, err := NormalizeDirectoryPath(path, true)
	if err != nil {
		return Config{}, err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return Config{}, err
	}
	for _, existing := range cfg.Directories {
		if existing == dir {
			return cfg, nil
		}
	}
	cfg.Directories = append(cfg.Directories, dir)
	if err := SaveConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func RemoveMonitoredDirectory(path string) (Config, error) {
	dir, err := NormalizeDirectoryPath(path, false)
	if err != nil {
		return Config{}, err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return Config{}, err
	}

	next := cfg.Directories[:0]
	for _, existing := range cfg.Directories {
		normalized, err := NormalizeDirectoryPath(existing, false)
		if err != nil || normalized != dir {
			next = append(next, existing)
		}
	}
	cfg.Directories = slices.Clone(next)
	if err := SaveConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func NormalizeDirectoryPath(path string, requireExists bool) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("directory path required")
	}

	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve directory path: %w", err)
	}
	abs = filepath.Clean(abs)

	if requireExists {
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("stat directory: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", abs)
		}
	}
	return abs, nil
}

func parseConfig(data []byte) (Config, error) {
	var cfg Config
	lines := strings.Split(string(data), "\n")
	inDirectories := false
	seenDirectories := false

	for _, line := range lines {
		line = strings.TrimSpace(stripTOMLComment(line))
		if line == "" {
			continue
		}

		if inDirectories {
			values, err := parseQuotedValues(line)
			if err != nil {
				return Config{}, err
			}
			cfg.Directories = append(cfg.Directories, values...)
			if strings.Contains(line, "]") {
				inDirectories = false
			}
			continue
		}

		if !strings.HasPrefix(line, "directories") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "directories" {
			return Config{}, fmt.Errorf("invalid directories assignment")
		}
		seenDirectories = true
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(value, "[") {
			return Config{}, fmt.Errorf("directories must be a TOML array")
		}
		values, err := parseQuotedValues(value)
		if err != nil {
			return Config{}, err
		}
		cfg.Directories = append(cfg.Directories, values...)
		if !strings.Contains(value, "]") {
			inDirectories = true
		}
	}

	if inDirectories {
		return Config{}, fmt.Errorf("unterminated directories array")
	}
	if !seenDirectories {
		cfg.Directories = nil
	}
	return cfg, nil
}

func stripTOMLComment(line string) string {
	inString := false
	escaped := false
	for i, r := range line {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		if r == '"' {
			inString = true
			continue
		}
		if r == '#' {
			return line[:i]
		}
	}
	return line
}

func parseQuotedValues(s string) ([]string, error) {
	var values []string
	for i := 0; i < len(s); i++ {
		if s[i] != '"' {
			continue
		}
		j := i + 1
		escaped := false
		for ; j < len(s); j++ {
			if escaped {
				escaped = false
				continue
			}
			if s[j] == '\\' {
				escaped = true
				continue
			}
			if s[j] == '"' {
				break
			}
		}
		if j >= len(s) {
			return nil, fmt.Errorf("unterminated quoted string")
		}
		value, err := strconv.Unquote(s[i : j+1])
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		i = j
	}
	return values, nil
}

func uniqueDirectories(dirs []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, dir := range dirs {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "." || dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		result = append(result, dir)
	}
	return result
}
