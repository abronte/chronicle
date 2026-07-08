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
	Directories    []string
	IgnorePatterns []string
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
	cfg.IgnorePatterns = uniqueIgnorePatterns(cfg.IgnorePatterns)
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
	cfg.IgnorePatterns = uniqueIgnorePatterns(cfg.IgnorePatterns)
	var sb strings.Builder
	sb.WriteString("# Chronicle system-wide configuration.\n")
	sb.WriteString("directories = [\n")
	for _, dir := range cfg.Directories {
		sb.WriteString("  ")
		sb.WriteString(strconv.Quote(dir))
		sb.WriteString(",\n")
	}
	sb.WriteString("]\n")
	sb.WriteString("\nignore_patterns = [\n")
	for _, pattern := range cfg.IgnorePatterns {
		sb.WriteString("  ")
		sb.WriteString(strconv.Quote(pattern))
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

func AddIgnorePattern(pattern string) (Config, error) {
	pattern, err := NormalizeIgnorePattern(pattern)
	if err != nil {
		return Config{}, err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return Config{}, err
	}
	for _, existing := range cfg.IgnorePatterns {
		if existing == pattern {
			return cfg, nil
		}
	}
	cfg.IgnorePatterns = append(cfg.IgnorePatterns, pattern)
	if err := SaveConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func RemoveIgnorePattern(pattern string) (Config, error) {
	pattern, err := NormalizeIgnorePattern(pattern)
	if err != nil {
		return Config{}, err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return Config{}, err
	}

	next := cfg.IgnorePatterns[:0]
	for _, existing := range cfg.IgnorePatterns {
		normalized, err := NormalizeIgnorePattern(existing)
		if err != nil || normalized != pattern {
			next = append(next, existing)
		}
	}
	cfg.IgnorePatterns = slices.Clone(next)
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

func NormalizeIgnorePattern(pattern string) (string, error) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return "", fmt.Errorf("ignore pattern required")
	}
	if strings.HasPrefix(pattern, "!") {
		return "", fmt.Errorf("negated ignore patterns are not supported")
	}
	return pattern, nil
}

func parseConfig(data []byte) (Config, error) {
	var cfg Config
	lines := strings.Split(string(data), "\n")
	arrayKey := ""

	for _, line := range lines {
		line = strings.TrimSpace(stripTOMLComment(line))
		if line == "" {
			continue
		}

		if arrayKey != "" {
			values, err := parseQuotedValues(line)
			if err != nil {
				return Config{}, err
			}
			appendConfigValues(&cfg, arrayKey, values)
			if strings.Contains(line, "]") {
				arrayKey = ""
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if key != "directories" && key != "ignore_patterns" {
			continue
		}

		if !ok {
			return Config{}, fmt.Errorf("invalid %s assignment", key)
		}
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(value, "[") {
			return Config{}, fmt.Errorf("%s must be a TOML array", key)
		}
		values, err := parseQuotedValues(value)
		if err != nil {
			return Config{}, err
		}
		appendConfigValues(&cfg, key, values)
		if !strings.Contains(value, "]") {
			arrayKey = key
		}
	}

	if arrayKey != "" {
		return Config{}, fmt.Errorf("unterminated %s array", arrayKey)
	}
	return cfg, nil
}

func appendConfigValues(cfg *Config, key string, values []string) {
	switch key {
	case "directories":
		cfg.Directories = append(cfg.Directories, values...)
	case "ignore_patterns":
		cfg.IgnorePatterns = append(cfg.IgnorePatterns, values...)
	}
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

func uniqueIgnorePatterns(patterns []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" || seen[pattern] {
			continue
		}
		seen[pattern] = true
		result = append(result, pattern)
	}
	return result
}
