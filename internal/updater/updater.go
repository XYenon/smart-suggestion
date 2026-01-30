package updater

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

var osExecutable = os.Executable

type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

var githubAPIURL = "https://api.github.com/repos/XYenon/smart-suggestion/releases/latest"

func CheckUpdate(currentVersion string) (string, string, error) {
	if currentVersion == "dev" {
		return "", "", fmt.Errorf("cannot update development version. Please install from releases")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(githubAPIURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("GitHub API error: %d %s", resp.StatusCode, string(body))
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	currentSemver := "v" + strings.TrimPrefix(currentVersion, "v")
	latestSemver := "v" + latestVersion

	if semver.IsValid(currentSemver) && semver.IsValid(latestSemver) {
		if semver.Compare(currentSemver, latestSemver) >= 0 {
			return latestVersion, "", nil
		}
	} else if latestVersion == strings.TrimPrefix(currentVersion, "v") {
		return latestVersion, "", nil
	}

	platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	expectedAssetName := fmt.Sprintf("smart-suggestion-%s.tar.gz", platform)
	for _, asset := range release.Assets {
		if asset.Name == expectedAssetName {
			if !strings.HasPrefix(asset.BrowserDownloadURL, "https://") {
				return "", "", fmt.Errorf("insecure download URL: %s", asset.BrowserDownloadURL)
			}
			return latestVersion, asset.BrowserDownloadURL, nil
		}
	}

	return latestVersion, "", fmt.Errorf("no release found for platform %s", platform)
}

func InstallUpdate(downloadURL string) error {
	tempDir, err := os.MkdirTemp("", "smart-suggestion-update")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	tempFile := filepath.Join(tempDir, "update.tar.gz")
	if err := downloadFile(downloadURL, tempFile); err != nil {
		return err
	}

	extractDir := filepath.Join(tempDir, "extracted")
	if err := extractTarGz(tempFile, extractDir); err != nil {
		return err
	}

	currentBinary, err := osExecutable()
	if err != nil {
		return err
	}

	newBinary, ok := findExtractedAsset(extractDir, "smart-suggestion")
	if !ok {
		return fmt.Errorf("failed to locate extracted binary")
	}

	pluginInstallPath := filepath.Join(filepath.Dir(currentBinary), "smart-suggestion.plugin.zsh")

	newPluginPath, ok := findExtractedAsset(extractDir, "smart-suggestion.plugin.zsh")
	if !ok {
		return fmt.Errorf("failed to locate extracted plugin")
	}

	binaryBackup := currentBinary + ".backup"
	pluginBackup := pluginInstallPath + ".backup"
	binaryBackedUp := false
	pluginBackedUp := false

	defer func() {
		if binaryBackedUp {
			_ = os.Remove(binaryBackup)
		}
		if pluginBackedUp {
			_ = os.Remove(pluginBackup)
		}
	}()

	if _, err := os.Stat(currentBinary); err == nil {
		if err := os.Rename(currentBinary, binaryBackup); err != nil {
			return fmt.Errorf("failed to backup binary: %w", err)
		}
		binaryBackedUp = true
	}

	if err := copyFile(newBinary, currentBinary); err != nil {
		if binaryBackedUp {
			_ = os.Rename(binaryBackup, currentBinary)
			binaryBackedUp = false
		}
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	if err := os.Chmod(currentBinary, 0755); err != nil {
		if binaryBackedUp {
			_ = os.Remove(currentBinary)
			_ = os.Rename(binaryBackup, currentBinary)
			binaryBackedUp = false
		}
		return fmt.Errorf("failed to set binary permissions: %w", err)
	}

	if _, err := os.Stat(pluginInstallPath); err == nil {
		if err := os.Rename(pluginInstallPath, pluginBackup); err != nil {
			if binaryBackedUp {
				_ = os.Remove(currentBinary)
				_ = os.Rename(binaryBackup, currentBinary)
				binaryBackedUp = false
			}
			return fmt.Errorf("failed to backup plugin: %w", err)
		}
		pluginBackedUp = true
	}

	if err := copyFile(newPluginPath, pluginInstallPath); err != nil {
		if pluginBackedUp {
			_ = os.Rename(pluginBackup, pluginInstallPath)
			pluginBackedUp = false
		}
		if binaryBackedUp {
			_ = os.Remove(currentBinary)
			_ = os.Rename(binaryBackup, currentBinary)
			binaryBackedUp = false
		}
		return fmt.Errorf("failed to install plugin: %w", err)
	}

	if err := os.Chmod(pluginInstallPath, 0644); err != nil {
		if pluginBackedUp {
			_ = os.Remove(pluginInstallPath)
			_ = os.Rename(pluginBackup, pluginInstallPath)
			pluginBackedUp = false
		}
		if binaryBackedUp {
			_ = os.Remove(currentBinary)
			_ = os.Rename(binaryBackup, currentBinary)
			binaryBackedUp = false
		}
		return fmt.Errorf("failed to set plugin permissions: %w", err)
	}

	return nil
}

func replaceWithBackup(targetPath, sourcePath string, mode os.FileMode) error {
	backupPath := targetPath + ".backup"
	if err := os.Rename(targetPath, backupPath); err != nil {
		return err
	}

	if err := copyFile(sourcePath, targetPath); err != nil {
		_ = os.Rename(backupPath, targetPath)
		return err
	}

	if err := os.Chmod(targetPath, mode); err != nil {
		_ = os.Rename(backupPath, targetPath)
		return err
	}

	_ = os.Remove(backupPath)
	return nil
}

func findExtractedAsset(extractDir, filename string) (string, bool) {
	direct := filepath.Join(extractDir, filename)
	if _, err := os.Stat(direct); err == nil {
		return direct, true
	}

	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(extractDir, entry.Name(), filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}

	return "", false
}

func downloadFile(url, filepath string) error {
	client := &http.Client{Timeout: 60 * time.Second}

	for attempt := 0; attempt < 3; attempt++ {
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			time.Sleep(time.Duration(1<<attempt) * time.Second)
			continue
		}

		file, err := os.Create(filepath)
		if err != nil {
			resp.Body.Close()
			return err
		}

		_, err = io.Copy(file, resp.Body)
		resp.Body.Close()
		file.Close()

		if err != nil {
			time.Sleep(time.Duration(1<<attempt) * time.Second)
			continue
		}

		return nil
	}
	return fmt.Errorf("download failed after 3 attempts")
}

func extractTarGz(src, dest string) error {
	file, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source archive: %w", err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		path, err := safeJoinPath(destAbs, header.Name)
		if err != nil {
			return fmt.Errorf("unsafe path in archive: %w", err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return fmt.Errorf("failed to open file for writing: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("failed to copy content: %w", err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("failed to close file: %w", err)
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("archive contains unsupported link type: %s", header.Name)
		}
	}
	return nil
}

func safeJoinPath(dest, name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))

	if cleaned == "." || cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("invalid path: %q", name)
	}
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("absolute path in archive: %q", name)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal in archive: %q", name)
	}

	full := filepath.Join(dest, cleaned)

	rel, err := filepath.Rel(dest, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes destination: %q", name)
	}

	return full, nil
}

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	_, err = io.Copy(d, s)
	return err
}
