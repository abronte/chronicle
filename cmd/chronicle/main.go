package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chronicle/internal"

	"github.com/fsnotify/fsnotify"
)

const version = "0.1.0"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	cmd := "watch"
	cmdArgs := args

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		cmdArgs = args[1:]
	}

	switch cmd {
	case "watch":
		return runWatch(cmdArgs, stdout)
	case "help":
		printHelp(stdout)
		return nil
	case "recent":
		return runRecent(cmdArgs, stdout)
	case "diffs":
		return runDiffs(cmdArgs, stdout)
	default:
		printHelp(stdout)
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func printHelp(stdout io.Writer) {
	fmt.Fprintf(stdout, "Chronicle %s - file change tracker\n\n", version)
	fmt.Fprintln(stdout, "Commands:")
	fmt.Fprintln(stdout, "  watch    Watch files for changes (default)")
	fmt.Fprintln(stdout, "  recent   Show last 10 files changed")
	fmt.Fprintln(stdout, "  diffs    Show last 5 diffs for a file")
	fmt.Fprintln(stdout, "  help     Print this help")
	fmt.Fprintln(stdout, "\nOptions:")
	fmt.Fprintln(stdout, "  -version, -v  Print version and exit")
}

func runWatch(args []string, stdout io.Writer) error {
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
		fmt.Fprintf(stdout, "chronicle %s\n", version)
		return nil
	}

	if err := internal.InitializeDB("."); err != nil {
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

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Remove == fsnotify.Remove {
				continue
			}
			info, err := os.Stat(event.Name)
			isDir := err == nil && info.IsDir()
			if !isDir {
				size, err := internal.GetFileSize(event.Name)
				if err != nil {
					continue
				}
				if size > 5*1024*1024 {
					continue
				}
				ascii, err := internal.IsAscii(event.Name)
				if err != nil || !ascii {
					continue
				}
				data, err := os.ReadFile(event.Name)
				if err != nil {
					continue
				}
				sha, err := internal.AddChange(event.Name, string(data))
				if err != nil {
					fmt.Fprintf(stdout, "chronicle: %v\n", err)
					continue
				}
				if sha != "" {
					fmt.Fprintf(stdout, "%s  %s\n", sha[:8], event.Name)
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

func runRecent(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("chronicle recent", flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: chronicle recent [options]\n\n")
		fmt.Fprintln(flags.Output(), "Options:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	if err := internal.InitializeDB("."); err != nil {
		return err
	}

	changes, err := internal.GetRecentChanges(10)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		fmt.Fprintln(stdout, "No changes recorded yet.")
		return nil
	}

	for _, c := range changes {
		t := time.UnixMilli(c.CreatedAt).Format("2006-01-02 15:04:05")
		fmt.Fprintf(stdout, "%s  %s\n", t, c.FilePath)
	}
	return nil
}

func addDirsRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name != "." && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
}

func runDiffs(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("chronicle diffs", flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: chronicle diffs <file>\n\n")
		fmt.Fprintln(flags.Output(), "Show the last 5 diffs for a file.")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() < 1 {
		flags.Usage()
		return fmt.Errorf("file path required")
	}
	filePath := flags.Arg(0)

	if err := internal.InitializeDB("."); err != nil {
		return err
	}

	changes, err := internal.GetFileHistory(filePath, 6)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		fmt.Fprintf(stdout, "No changes recorded for %s.\n", filePath)
		return nil
	}

	if len(changes) == 1 {
		t := time.UnixMilli(changes[0].CreatedAt).Format("2006-01-02 15:04:05")
		fmt.Fprintf(stdout, "%s  %s\n", t, changes[0].SHA[:8])
		fmt.Fprintf(stdout, "--- /dev/null\n+++ %s\n", changes[0].FilePath)
		for _, line := range strings.Split(changes[0].Data, "\n") {
			fmt.Fprintf(stdout, "+%s\n", line)
		}
		return nil
	}

	for i := len(changes) - 2; i >= 0; i-- {
		older := changes[i+1]
		newer := changes[i]
		diff := internal.CreateDiff(older.Data, newer.Data)
		if diff == "" {
			continue
		}
		t := time.UnixMilli(newer.CreatedAt).Format("2006-01-02 15:04:05")
		fmt.Fprintf(stdout, "%s  %s\n", t, newer.SHA[:8])
		fmt.Fprintf(stdout, "--- %s\n+++ %s\n", filePath, filePath)
		fmt.Fprint(stdout, diff)
	}

	return nil
}
