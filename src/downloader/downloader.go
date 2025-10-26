package downloader

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/urixen-org/minecraft-launcher-core/src/events"
)

// ------------------ Structs ------------------

type Manifest struct {
	Versions []Version `json:"versions"`
}

type Version struct {
	Id          string `json:"id"`
	Type        string `json:"type"`
	Url         string `json:"url"`
	Time        string `json:"time"`
	ReleaseTime string `json:"releaseTime"`
}

type VersionMetadata struct {
	Downloads struct {
		Client struct {
			Url string `json:"url"`
		} `json:"client"`
	} `json:"downloads"`

	AssetIndex struct {
		Id  string `json:"id"`
		Url string `json:"url"`
	} `json:"assetIndex"`

	Libraries []struct {
		Name      string `json:"name"`
		Downloads struct {
			Artifact struct {
				Url  string `json:"url"`
				Sha1 string `json:"sha1"`
				Path string `json:"path"`
			} `json:"artifact"`
			Classifiers map[string]struct {
				Url  string `json:"url"`
				Sha1 string `json:"sha1"`
				Path string `json:"path"`
			} `json:"classifiers"`
		} `json:"downloads"`
		Rules []struct {
			Action string `json:"action"`
			OS     struct {
				Name string `json:"name"`
			} `json:"os"`
		} `json:"rules"`
		Natives map[string]string `json:"natives"`
	} `json:"libraries"`
}

type AssetIndex struct {
	Objects map[string]struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	} `json:"objects"`
}

// ------------------ Global Event Emitter ------------------

var E *events.EventEmitter

// ------------------ Helpers ------------------

func DownloadFile(file string, url string, E *events.EventEmitter) error {
	if _, err := os.Stat(file); err == nil {
		E.Emit("file_exists", file)
		return nil
	}

	resp, err := http.Get(url)
	if err != nil {
		E.Emit("error", "Failed to download "+file+": "+err.Error())
		return err
	}
	defer resp.Body.Close()

	os.MkdirAll(filepath.Dir(file), 0755)

	out, err := os.Create(file)
	if err != nil {
		E.Emit("error", "Failed to create file "+file+": "+err.Error())
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		E.Emit("error", "Failed to write file "+file+": "+err.Error())
	} else {
		E.Emit("file_downloaded", file)
	}
	return err
}

func getOSName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "osx"
	case "linux":
		return "linux"
	default:
		return runtime.GOOS
	}
}

func shouldIncludeLibrary(rules []struct {
	Action string `json:"action"`
	OS     struct {
		Name string `json:"name"`
	} `json:"os"`
}) bool {
	if len(rules) == 0 {
		return true
	}

	allowed := false
	osName := getOSName()

	for _, rule := range rules {
		if rule.Action == "allow" {
			if rule.OS.Name == "" || rule.OS.Name == osName {
				allowed = true
			}
		} else if rule.Action == "disallow" {
			if rule.OS.Name == "" || rule.OS.Name == osName {
				return false
			}
		}
	}
	return allowed
}

// ------------------ Libraries ------------------

func DownloadLibraries(metadata VersionMetadata, mcDir string, E *events.EventEmitter) {
	libDir := filepath.Join(mcDir, "libraries")
	osName := getOSName()

	for _, lib := range metadata.Libraries {
		// Check if library should be included based on rules
		if !shouldIncludeLibrary(lib.Rules) {
			E.Emit("library_skipped", lib.Name+" (OS rules)")
			continue
		}

		// Download main artifact
		if lib.Downloads.Artifact.Url != "" && lib.Downloads.Artifact.Path != "" {
			url := lib.Downloads.Artifact.Url
			path := filepath.Join(libDir, filepath.FromSlash(lib.Downloads.Artifact.Path))

			E.Emit("library_download_start", lib.Name)
			if err := DownloadFile(path, url, E); err != nil {
				E.Emit("library_failed", lib.Name)
			} else {
				E.Emit("library_done", lib.Name)
			}
		}

		// Download natives (classifiers)
		if lib.Downloads.Classifiers != nil && len(lib.Downloads.Classifiers) > 0 {
			// Determine native key for this OS
			var nativeKey string
			if osName == "windows" {
				if runtime.GOARCH == "amd64" {
					nativeKey = "natives-windows"
				} else {
					nativeKey = "natives-windows-32"
				}
			} else if osName == "osx" {
				nativeKey = "natives-osx"
			} else if osName == "linux" {
				nativeKey = "natives-linux"
			}

			// Download the native classifier
			for classifierName, classifier := range lib.Downloads.Classifiers {
				if strings.Contains(classifierName, nativeKey) || classifierName == nativeKey {
					if classifier.Url != "" && classifier.Path != "" {
						path := filepath.Join(libDir, filepath.FromSlash(classifier.Path))
						E.Emit("library_download_start", lib.Name+" ("+classifierName+")")
						if err := DownloadFile(path, classifier.Url, E); err != nil {
							E.Emit("library_failed", lib.Name+" (native)")
						} else {
							E.Emit("library_done", lib.Name+" (native)")
						}
					}
				}
			}
		} else if lib.Downloads.Artifact.Url == "" {
			E.Emit("library_skipped", lib.Name+" (no artifact URL)")
		}
	}
}

// ------------------ Assets ------------------

func DownloadAssets(metadata VersionMetadata, mcDir string, E *events.EventEmitter) {
	// Download asset index
	resp, err := http.Get(metadata.AssetIndex.Url)
	if err != nil {
		E.Emit("error", "Failed to fetch asset index: "+err.Error())
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	var index AssetIndex
	json.Unmarshal(data, &index)

	objectsDir := filepath.Join(mcDir, "assets", "objects")

	for _, asset := range index.Objects {
		hash := asset.Hash
		sub := hash[:2]

		url := "https://resources.download.minecraft.net/" + sub + "/" + hash
		path := filepath.Join(objectsDir, sub, hash)

		E.Emit("asset_download_start", hash)
		_ = DownloadFile(path, url, E)
	}

	E.Emit("assets_done", nil)
}

// ------------------ Version Download ------------------

func DownloadVersion(version string, mcDir string, E *events.EventEmitter) {
	E.Emit("version_download_start", version)

	// Fetch version manifest
	resp, err := http.Get("https://launchermeta.mojang.com/mc/game/version_manifest.json")
	if err != nil {
		E.Emit("error", "Failed to fetch version manifest: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		E.Emit("error", "Failed to read manifest body: "+err.Error())
		return
	}

	var manifest Manifest
	json.Unmarshal(body, &manifest)

	// Find version
	var selected *Version
	for _, v := range manifest.Versions {
		if v.Id == version {
			selected = &v
			break
		}
	}

	if selected == nil {
		E.Emit("version_not_found", version)
		return
	}

	// Download metadata
	metaResp, err := http.Get(selected.Url)
	if err != nil {
		E.Emit("error", "Failed to fetch version metadata: "+err.Error())
		return
	}
	defer metaResp.Body.Close()

	metaBody, _ := io.ReadAll(metaResp.Body)
	var metadata VersionMetadata
	json.Unmarshal(metaBody, &metadata)

	// Download client jar
	jarPath := filepath.Join(mcDir, "versions", version, version+".jar")
	metadataPath := filepath.Join(mcDir, "versions", version, version+".json")
	E.Emit("client_download_start", jarPath)
	_ = DownloadFile(jarPath, metadata.Downloads.Client.Url, E)

	_ = os.WriteFile(metadataPath, metaBody, 0644)
	E.Emit("metadata_saved", metadataPath)

	// Download libraries (includes natives now!)
	DownloadLibraries(metadata, mcDir, E)

	// Download assets
	DownloadAssets(metadata, mcDir, E)

	E.Emit("version_downloaded", version)
}
