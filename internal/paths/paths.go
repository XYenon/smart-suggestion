package paths

import (
	"os"
	"path/filepath"
)

const ProxyLogFilename = "proxy.log"

func GetCacheDir() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "smart-suggestion")
		}
		cacheDir = filepath.Join(homeDir, ".cache")
	}
	return filepath.Join(cacheDir, "smart-suggestion")
}

func GetDefaultProxyLogFile() string {
	return filepath.Join(GetCacheDir(), ProxyLogFilename)
}
