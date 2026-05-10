package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const repo = "abronte/chronicle"

func CheckUpdate(currentVersion string) (string, error) {
	release, err := getLatestRelease()
	if err != nil {
		return "", err
	}

	if release.TagName == "" {
		return "", nil
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	if compareVersions(latest, currentVersion) <= 0 {
		return "", nil
	}

	return release.TagName, nil
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func getLatestRelease() (*release, error) {
	resp, err := http.Get(fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo))
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch release: unexpected status %d", resp.StatusCode)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("fetch release: decode response: %w", err)
	}

	return &rel, nil
}

func InstallUpdate(currentVersion string) error {
	rel, err := getLatestRelease()
	if err != nil {
		return err
	}

	latest := strings.TrimPrefix(rel.TagName, "v")
	if compareVersions(latest, currentVersion) <= 0 {
		return fmt.Errorf("already up to date (%s)", currentVersion)
	}

	assetName := fmt.Sprintf("chronicle-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}

	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no download found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download binary: unexpected status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "chronicle-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write binary: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("set executable permissions: %w", err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}

	return nil
}

func compareVersions(a, b string) int {
	aparts := strings.Split(a, ".")
	bparts := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var av, bv int
		if i < len(aparts) {
			av, _ = strconv.Atoi(aparts[i])
		}
		if i < len(bparts) {
			bv, _ = strconv.Atoi(bparts[i])
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
