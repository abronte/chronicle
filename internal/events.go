package internal

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

var ignorePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(^|/)\.([^/]+)`),
}

var gitignorePatterns []*regexp.Regexp

func shouldIgnore(path string) bool {
	clean := filepath.ToSlash(strings.TrimPrefix(path, "./"))
	return shouldIgnoreRelative(clean, gitignorePatterns)
}

func shouldIgnoreRelative(path string, patterns []*regexp.Regexp) bool {
	clean := filepath.ToSlash(strings.TrimPrefix(path, "./"))
	for _, re := range ignorePatterns {
		if re.MatchString(clean) {
			return true
		}
	}
	for _, re := range patterns {
		if re.MatchString(clean) {
			return true
		}
	}
	return false
}

func shouldIgnoreForRoot(root, path string, patterns []*regexp.Regexp) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return shouldIgnoreRelative(rel, patterns)
}

func Watch(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("chronicle watch", flag.ContinueOnError)
	flags.SetOutput(stdout)

	showVersion := flags.Bool("version", false, "print version and exit")
	flags.BoolVar(showVersion, "v", false, "print version and exit")
	webEnabled := flags.Bool("web", true, "start web interface")
	webAddr := flags.String("addr", DefaultWebAddress, "web server address")

	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: chronicle watch [options]\n\n")
		fmt.Fprintln(flags.Output(), "Options:")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Fprintf(stdout, "chronicle %s\n", Version)
		return nil
	}

	if err := InitializeCentralDB(); err != nil {
		return err
	}

	if _, err := LoadConfig(); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	state := newWatchState(watcher)
	reload := func() {
		cfg, err := LoadConfig()
		if err != nil {
			fmt.Fprintf(stdout, "chronicle: load config: %v\n", err)
			return
		}
		if err := state.Sync(cfg); err != nil {
			fmt.Fprintf(stdout, "chronicle: sync watched directories: %v\n", err)
		}
	}
	reload()

	serverErr := make(chan error, 1)
	if *webEnabled {
		go func() {
			serverErr <- ServeWeb(*webAddr)
		}()
		log.Printf("web interface listening on http://localhost%s", *webAddr)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			state.HandleEvent(event, stdout)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return err
		case err := <-serverErr:
			if err != nil {
				return err
			}
			return nil
		case <-ticker.C:
			reload()
		}
	}
}

func loadGitignore(root string) error {
	patterns, err := loadGitignorePatterns(root)
	if err != nil {
		return err
	}
	gitignorePatterns = append(gitignorePatterns, patterns...)
	return nil
}

func loadGitignorePatterns(root string) ([]*regexp.Regexp, error) {
	f, err := os.Open(filepath.Join(root, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var patterns []*regexp.Regexp
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		re := gitignoreToRegex(line)
		if re != nil {
			patterns = append(patterns, re)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return patterns, nil
}

func gitignoreToRegex(pattern string) *regexp.Regexp {
	if strings.HasPrefix(pattern, "!") {
		return nil
	}

	dirOnly := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimRight(pattern, "/")

	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")

	if pattern == "" {
		return nil
	}

	escaped := regexp.QuoteMeta(pattern)
	escaped = strings.ReplaceAll(escaped, `\*\*/`, `__DSLASH__`)
	escaped = strings.ReplaceAll(escaped, `\*\*`, `.*`)
	escaped = strings.ReplaceAll(escaped, `__DSLASH__`, `(.*/)?`)
	escaped = strings.ReplaceAll(escaped, `\*`, `[^/]*`)
	escaped = strings.ReplaceAll(escaped, `\?`, `[^/]`)

	var prefix string
	if anchored {
		prefix = `^`
	} else {
		prefix = `^(.*/)?`
	}

	suffix := `$`
	if dirOnly {
		suffix = `(/.*)?$`
	}

	return regexp.MustCompile(prefix + escaped + suffix)
}

func compileIgnorePatterns(patterns []string) []*regexp.Regexp {
	var result []*regexp.Regexp
	for _, pattern := range uniqueIgnorePatterns(patterns) {
		re := gitignoreToRegex(pattern)
		if re != nil {
			result = append(result, re)
		}
	}
	return result
}

func ignorePatternsKey(patterns []string) string {
	return strings.Join(uniqueIgnorePatterns(patterns), "\x00")
}

func addDirsRecursive(watcher *fsnotify.Watcher, root string) error {
	return addDirsRecursiveWithPatterns(watcher, root, gitignorePatterns)
}

func addDirsRecursiveWithPatterns(watcher *fsnotify.Watcher, root string, patterns []*regexp.Regexp) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if shouldIgnoreForRoot(root, path, patterns) {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
}

type watchedRoot struct {
	path            string
	patterns        []*regexp.Regexp
	globalIgnoreKey string
}

type watchState struct {
	watcher     *fsnotify.Watcher
	roots       map[string]*watchedRoot
	watchedDirs map[string]string
}

func newWatchState(watcher *fsnotify.Watcher) *watchState {
	return &watchState{
		watcher:     watcher,
		roots:       map[string]*watchedRoot{},
		watchedDirs: map[string]string{},
	}
}

func (s *watchState) Sync(cfg Config) error {
	targets := map[string]bool{}
	var firstErr error
	globalPatterns := uniqueIgnorePatterns(cfg.IgnorePatterns)
	globalIgnoreKey := ignorePatternsKey(globalPatterns)

	for _, dir := range cfg.Directories {
		normalized, err := NormalizeDirectoryPath(dir, true)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		targets[normalized] = true
	}

	for root, watched := range s.roots {
		if !targets[root] || watched.globalIgnoreKey != globalIgnoreKey {
			s.removeRoot(root)
		}
	}

	var ordered []string
	for root := range targets {
		ordered = append(ordered, root)
	}
	slices.Sort(ordered)
	for _, root := range ordered {
		if _, ok := s.roots[root]; ok {
			continue
		}
		if err := s.addRoot(root, globalPatterns); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (s *watchState) addRoot(root string, globalPatterns []string) error {
	patterns, err := loadGitignorePatterns(root)
	if err != nil {
		return fmt.Errorf("load gitignore for %s: %w", root, err)
	}
	combinedPatterns := append(compileIgnorePatterns(globalPatterns), patterns...)
	watched := &watchedRoot{
		path:            root,
		patterns:        combinedPatterns,
		globalIgnoreKey: ignorePatternsKey(globalPatterns),
	}
	s.roots[root] = watched
	if err := s.addDirsRecursive(watched, root); err != nil {
		return err
	}
	log.Printf("watching directory: %s", root)
	return nil
}

func (s *watchState) removeRoot(root string) {
	for dir, owner := range s.watchedDirs {
		if owner != root {
			continue
		}
		_ = s.watcher.Remove(dir)
		delete(s.watchedDirs, dir)
	}
	delete(s.roots, root)
	log.Printf("stopped watching directory: %s", root)
}

func (s *watchState) addDirsRecursive(root *watchedRoot, start string) error {
	start, err := filepath.Abs(start)
	if err != nil {
		return err
	}
	return filepath.Walk(start, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		if shouldIgnoreForRoot(root.path, path, root.patterns) {
			return filepath.SkipDir
		}
		if err := s.watcher.Add(path); err != nil {
			return err
		}
		s.watchedDirs[path] = root.path
		return nil
	})
}

func (s *watchState) rootForPath(path string) *watchedRoot {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil
	}

	var best *watchedRoot
	for _, root := range s.roots {
		if !isInsideRoot(root.path, path) {
			continue
		}
		if best == nil || len(root.path) > len(best.path) {
			best = root
		}
	}
	return best
}

func (s *watchState) HandleEvent(event fsnotify.Event, stdout io.Writer) {
	root := s.rootForPath(event.Name)
	if root == nil || shouldIgnoreForRoot(root.path, event.Name, root.patterns) {
		return
	}

	if event.Op&fsnotify.Remove == fsnotify.Remove || event.Op&fsnotify.Rename == fsnotify.Rename {
		s.handleDeleteEvent(root, event.Name, stdout)
		return
	}

	info, err := os.Stat(event.Name)
	if err != nil {
		return
	}
	if info.IsDir() {
		if event.Op&fsnotify.Create == fsnotify.Create {
			_ = s.addDirsRecursive(root, event.Name)
		}
		return
	}

	size, err := GetFileSize(event.Name)
	if err != nil || size > 5*1024*1024 {
		return
	}
	ascii, err := IsAscii(event.Name)
	if err != nil || !ascii {
		return
	}
	data, err := os.ReadFile(event.Name)
	if err != nil {
		return
	}
	sha, err := AddChangeForDirectory(root.path, event.Name, string(data))
	if err != nil {
		fmt.Fprintf(stdout, "chronicle: %v\n", err)
		return
	}
	if sha != "" {
		rel, _, _ := normalizeTrackedFilePath(root.path, event.Name)
		log.Printf("%s (%s)", filepath.Join(root.path, filepath.FromSlash(rel)), sha[:8])
	}
}

func (s *watchState) handleDeleteEvent(root *watchedRoot, path string, stdout io.Writer) {
	if s.isWatchedDir(path) {
		s.removeWatchedDirTree(path)
		return
	}

	sha, err := AddDeleteForDirectory(root.path, path)
	if err != nil {
		fmt.Fprintf(stdout, "chronicle: %v\n", err)
		return
	}
	if sha != "" {
		rel, _, _ := normalizeTrackedFilePath(root.path, path)
		log.Printf("%s deleted (%s)", filepath.Join(root.path, filepath.FromSlash(rel)), sha[:8])
	}
}

func (s *watchState) isWatchedDir(path string) bool {
	path, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	_, ok := s.watchedDirs[path]
	return ok
}

func (s *watchState) removeWatchedDirTree(path string) {
	path, err := filepath.Abs(path)
	if err != nil {
		return
	}
	for dir := range s.watchedDirs {
		if !isInsideRoot(path, dir) {
			continue
		}
		if s.watcher != nil {
			_ = s.watcher.Remove(dir)
		}
		delete(s.watchedDirs, dir)
	}
}

func isInsideRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}
