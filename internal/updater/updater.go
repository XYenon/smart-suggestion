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

	resp, err := http.Get(githubAPIURL)
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
	if latestVersion == currentVersion {
		return latestVersion, "", nil
	}

	platform := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, platform) {
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

	if err := replaceWithBackup(currentBinary, newBinary, 0755); err != nil {
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	if err := replaceWithBackup(pluginInstallPath, newPluginPath, 0644); err != nil {
		return fmt.Errorf("binary updated but failed to install plugin to %s: %w", pluginInstallPath, err)
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

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		path := filepath.Join(dest, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
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
		}
	}
	return nil
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
