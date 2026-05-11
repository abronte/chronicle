package internal

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/fsnotify/fsnotify"
)

var ignorePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(^|/)\.([^/]+)`),
	regexp.MustCompile(`(^|/)node_modules(/|$)`),
}

func shouldIgnore(path string) bool {
	for _, re := range ignorePatterns {
		if re.MatchString(path) {
			return true
		}
	}
	return false
}

func Watch(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("chronicle watch", flag.ContinueOnError)
	flags.SetOutput(stdout)

	showVersion := flags.Bool("version", false, "print version and exit")
	flags.BoolVar(showVersion, "v", false, "print version and exit")

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

	if err := InitializeDB("."); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := addDirsRecursive(watcher, "."); err != nil {
		return err
	}

	watchDir, _ := filepath.Abs(".")
	log.Printf("watching directory: %s", watchDir)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Remove == fsnotify.Remove {
				continue
			}
			if shouldIgnore(event.Name) {
				continue
			}
			info, err := os.Stat(event.Name)
			isDir := err == nil && info.IsDir()
			if !isDir {
				size, err := GetFileSize(event.Name)
				if err != nil {
					continue
				}
				if size > 5*1024*1024 {
					continue
				}
				ascii, err := IsAscii(event.Name)
				if err != nil || !ascii {
					continue
				}
				data, err := os.ReadFile(event.Name)
				if err != nil {
					continue
				}
				sha, err := AddChange(event.Name, string(data))
				if err != nil {
					fmt.Fprintf(stdout, "chronicle: %v\n", err)
					continue
				}
				if sha != "" {
					log.Printf("%s (%s)", event.Name, sha[:8])
				}
			}
			if event.Op&fsnotify.Create == fsnotify.Create && isDir {
				_ = addDirsRecursive(watcher, event.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return err
		}
	}
}

func addDirsRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if shouldIgnore(path) {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
}
