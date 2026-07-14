package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"chronicle/internal"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	go func() {
		if latest, err := internal.CheckUpdate(internal.Version); err == nil && latest != "" {
			fmt.Fprintf(os.Stderr, "A new version is available: %s\n", latest)
		}
	}()

	cmd := "watch"
	cmdArgs := args

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		cmdArgs = args[1:]
	}

	switch cmd {
	case "watch":
		return internal.Watch(cmdArgs, stdout)
	case "web":
		return runWeb(cmdArgs, stdout)
	case "help":
		printHelp(stdout)
		return nil
	case "recent":
		return runRecent(cmdArgs, stdout)
	case "diffs":
		return runDiffs(cmdArgs, stdout)
	case "restore":
		return runRestore(cmdArgs, stdout)
	case "update":
		return runUpdate(cmdArgs, stdout)
	default:
		printHelp(stdout)
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func printHelp(stdout io.Writer) {
	fmt.Fprintf(stdout, "Chronicle %s - file change tracker\n\n", internal.Version)
	fmt.Fprintln(stdout, "Commands:")
	fmt.Fprintln(stdout, "  watch    Watch configured directories and serve the web UI (default)")
	fmt.Fprintln(stdout, "  web      Serve the web UI without starting the watcher")
	fmt.Fprintln(stdout, "  recent   Show last 10 files changed")
	fmt.Fprintln(stdout, "  diffs    Show last 5 diffs for a file")
	fmt.Fprintln(stdout, "  restore  Restore a file from its recorded history")
	fmt.Fprintln(stdout, "  update   Download and install the latest version")
	fmt.Fprintln(stdout, "  help     Print this help")
	fmt.Fprintln(stdout, "\nOptions:")
	fmt.Fprintln(stdout, "  -version, -v  Print version and exit")
}

var errRestoreServiceUnavailable = errors.New("restore service unavailable")

func runRestore(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("chronicle restore", flag.ContinueOnError)
	flags.SetOutput(stdout)
	version := flags.String("version", "", "version SHA or unique SHA prefix (default: latest)")
	addr := flags.String("addr", internal.DefaultWebAddress, "running Chronicle web server address")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: chronicle restore [options] <file>\n\n")
		fmt.Fprintln(flags.Output(), "Restore a file from Chronicle's recorded history.")
		fmt.Fprintln(flags.Output(), "\nOptions:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() < 1 {
		flags.Usage()
		return fmt.Errorf("file path required")
	}
	if flags.NArg() > 1 {
		flags.Usage()
		return fmt.Errorf("only one file path may be restored")
	}

	absolutePath, err := internal.NormalizeFilePath(flags.Arg(0))
	if err != nil {
		return err
	}

	snapshot, serviceErr := getFileVersionFromService(*addr, absolutePath, *version)
	if serviceErr != nil && !errors.Is(serviceErr, errRestoreServiceUnavailable) {
		return serviceErr
	}
	if serviceErr != nil {
		if err := internal.InitializeCentralDB(); err != nil {
			return fmt.Errorf("%v; open history database directly: %w; restart the Chronicle watcher after upgrading, or pass -addr if it uses a custom web address", serviceErr, err)
		}
		change, err := internal.GetFileVersion(absolutePath, *version)
		if err != nil {
			return err
		}
		snapshot = internal.FileVersionResponse{
			AbsolutePath: change.AbsolutePath,
			SHA:          change.SHA,
			Data:         change.Data,
		}
	}

	if snapshot.AbsolutePath != absolutePath {
		return fmt.Errorf("history returned %s for requested file %s", snapshot.AbsolutePath, absolutePath)
	}
	if err := writeRestoredFile(absolutePath, snapshot.Data); err != nil {
		return err
	}

	versionLabel := snapshot.SHA
	if len(versionLabel) > 8 {
		versionLabel = versionLabel[:8]
	}
	fmt.Fprintf(stdout, "Restored %s to version %s.\n", absolutePath, versionLabel)
	return nil
}

func getFileVersionFromService(addr, filePath, version string) (internal.FileVersionResponse, error) {
	endpoint, err := fileVersionURL(addr, filePath, version)
	if err != nil {
		return internal.FileVersionResponse{}, err
	}

	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Get(endpoint)
	if err != nil {
		return internal.FileVersionResponse{}, fmt.Errorf("%w at %s: %v", errRestoreServiceUnavailable, addr, err)
	}
	defer response.Body.Close()

	if response.Header.Get(internal.FileVersionAPIHeader) != internal.FileVersionAPIHeaderName {
		return internal.FileVersionResponse{}, fmt.Errorf("%w at %s: running server does not support file restore", errRestoreServiceUnavailable, addr)
	}
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		if text := strings.TrimSpace(string(message)); text != "" {
			return internal.FileVersionResponse{}, errors.New(text)
		}
		return internal.FileVersionResponse{}, fmt.Errorf("restore service returned %s", response.Status)
	}

	var snapshot internal.FileVersionResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&snapshot); err != nil {
		return internal.FileVersionResponse{}, fmt.Errorf("decode restore response: %w", err)
	}
	return snapshot, nil
}

func fileVersionURL(addr, filePath, version string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = internal.DefaultWebAddress
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}

	endpoint, err := url.Parse(addr)
	if err != nil {
		return "", fmt.Errorf("parse Chronicle web server address: %w", err)
	}
	if endpoint.Scheme != "http" || endpoint.Host == "" {
		return "", fmt.Errorf("Chronicle web server address must be a local HTTP address")
	}
	host := endpoint.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("Chronicle web server address must be local")
	}

	endpoint.Path = internal.FileVersionAPIPath
	endpoint.RawPath = ""
	query := url.Values{"path": {filePath}}
	if strings.TrimSpace(version) != "" {
		query.Set("version", version)
	}
	endpoint.RawQuery = query.Encode()
	endpoint.Fragment = ""
	return endpoint.String(), nil
}

func writeRestoredFile(filePath, data string) error {
	parent := filepath.Dir(filePath)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create restore directory: %w", err)
	}

	mode := os.FileMode(0600)
	info, err := os.Stat(filePath)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("restore target %s is a directory", filePath)
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect restore target: %w", err)
	}

	temp, err := os.CreateTemp(parent, ".chronicle-restore-*")
	if err != nil {
		return fmt.Errorf("create temporary restore file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return fmt.Errorf("set restored file permissions: %w", err)
	}
	if _, err := io.WriteString(temp, data); err != nil {
		temp.Close()
		return fmt.Errorf("write restored file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync restored file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close restored file: %w", err)
	}
	if err := os.Rename(tempPath, filePath); err != nil {
		return fmt.Errorf("replace restored file: %w", err)
	}
	return nil
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

	if err := internal.InitializeCentralDB(); err != nil {
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
		if c.ChangeType == internal.ChangeTypeDelete {
			fmt.Fprintf(stdout, "%s  deleted  %s\n", t, c.AbsolutePath)
			continue
		}
		fmt.Fprintf(stdout, "%s  %s\n", t, c.AbsolutePath)
	}
	return nil
}

func runUpdate(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("chronicle update", flag.ContinueOnError)
	flags.SetOutput(stdout)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: chronicle update\n\n")
		fmt.Fprintln(flags.Output(), "Check for and install the latest version of chronicle.")
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Checking for updates...\n")
	if err := internal.InstallUpdate(internal.Version); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Updated to the latest version.\n")
	return nil
}

func runDiffs(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("chronicle diffs", flag.ContinueOnError)
	flags.SetOutput(stdout)
	dir := flags.String("dir", "", "monitored directory root for the file")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: chronicle diffs [options] <file>\n\n")
		fmt.Fprintln(flags.Output(), "Show the last 5 diffs for a file.")
		fmt.Fprintln(flags.Output(), "\nOptions:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	if flags.NArg() < 1 {
		flags.Usage()
		return fmt.Errorf("file path required")
	}
	filePath := flags.Arg(0)

	if err := internal.InitializeCentralDB(); err != nil {
		return err
	}

	var changes []internal.FileChange
	var err error
	if *dir != "" {
		changes, err = internal.GetFileHistoryForDirectory(*dir, filePath, 6)
	} else {
		changes, err = internal.GetFileHistory(filePath, 6)
	}
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
		if changes[0].ChangeType == internal.ChangeTypeDelete {
			fmt.Fprintf(stdout, "--- %s\n+++ /dev/null\n", changes[0].FilePath)
			fmt.Fprint(stdout, internal.CreateDiff(changes[0].Data, ""))
			return nil
		}
		fmt.Fprintf(stdout, "--- /dev/null\n+++ %s\n", changes[0].FilePath)
		for _, line := range strings.Split(changes[0].Data, "\n") {
			fmt.Fprintf(stdout, "+%s\n", line)
		}
		return nil
	}

	for i := len(changes) - 2; i >= 0; i-- {
		older := changes[i+1]
		newer := changes[i]
		diff, oldPath, newPath := diffForCLI(filePath, older, newer)
		if diff == "" {
			continue
		}
		t := time.UnixMilli(newer.CreatedAt).Format("2006-01-02 15:04:05")
		fmt.Fprintf(stdout, "%s  %s\n", t, newer.SHA[:8])
		fmt.Fprintf(stdout, "--- %s\n+++ %s\n", oldPath, newPath)
		fmt.Fprint(stdout, diff)
	}

	return nil
}

func diffForCLI(filePath string, older, newer internal.FileChange) (string, string, string) {
	switch {
	case newer.ChangeType == internal.ChangeTypeDelete:
		return internal.CreateDiff(newer.Data, ""), filePath, "/dev/null"
	case older.ChangeType == internal.ChangeTypeDelete:
		return internal.CreateDiff("", newer.Data), "/dev/null", filePath
	default:
		return internal.CreateDiff(older.Data, newer.Data), filePath, filePath
	}
}

func runWeb(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("chronicle web", flag.ContinueOnError)
	flags.SetOutput(stdout)
	addr := flags.String("addr", internal.DefaultWebAddress, "web server address")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: chronicle web [options]\n\n")
		fmt.Fprintln(flags.Output(), "Serve the Chronicle web interface.")
		fmt.Fprintln(flags.Output(), "\nOptions:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Chronicle web interface listening on http://localhost%s\n", *addr)
	return internal.ServeWeb(*addr)
}
