package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// -------------------- MC Directory --------------------

var mcDir string

func SetMCDir(dir string) {
	mcDir = dir
}

func GetMCDir() string {
	if mcDir != "" {
		return mcDir
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), ".minecraft")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "minecraft")
	default:
		return filepath.Join(os.Getenv("HOME"), ".minecraft")
	}
}

// -------------------- Path & File Helpers --------------------

func PathJoin(paths ...string) string {
	return filepath.Join(paths...)
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func EnsureDirExists(path string) error {
	if !DirExists(path) {
		return os.MkdirAll(path, 0755)
	}
	return nil
}

func DownloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", dest, err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// -------------------- Minecraft Versions --------------------

func GetAllVanillaMCVersions() ([]string, error) {
	const manifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest.json"

	resp, err := http.Get(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest struct {
		Versions []struct {
			ID string `json:"id"`
		} `json:"versions"`
	}

	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	versions := make([]string, len(manifest.Versions))
	for i, v := range manifest.Versions {
		versions[i] = v.ID
	}
	return versions, nil
}

func GetLatestMCVersion() (string, error) {
	const manifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest.json"

	resp, err := http.Get(manifestURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest struct {
		Latest struct {
			Release string `json:"release"`
		} `json:"latest"`
	}

	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", fmt.Errorf("failed to parse manifest: %w", err)
	}

	return manifest.Latest.Release, nil
}

// -------------------- Backups --------------------

func BackupFile(src, backup string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(backup)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
