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

// Manifest represents the structure of the Minecraft version manifest file.
type Manifest struct {
	Versions []Version `json:"versions"`
}

// Version represents a single version entry in the Minecraft manifest.
type Version struct {
	Id          string `json:"id"`
	Type        string `json:"type"`
	Url         string `json:"url"`
	Time        string `json:"time"`
	ReleaseTime string `json:"releaseTime"`
}

// VersionMetadata represents the detailed metadata for a specific Minecraft version.
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

// AssetIndex represents the structure of the Minecraft asset index file, mapping asset names to object hashes.
type AssetIndex struct {
	Objects map[string]struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	} `json:"objects"`
}

// ------------------ Global Event Emitter ------------------

// E is the global event emitter instance for emitting download progress and status updates.
var E *events.EventEmitter

// ------------------ Helpers ------------------

// DownloadFile downloads a file from a given URL to a specified file path.
// It checks if the file already exists before downloading and emits events for status.
// It creates the parent directories for the file if they don't exist.
func DownloadFile(file string, url string, E *events.EventEmitter) error {
	// Check if file already exists
	if _, err := os.Stat(file); err == nil {
		E.Emit("file_exists", file)
		return nil
	}

	// Start download
	resp, err := http.Get(url)
	if err != nil {
		E.Emit("error", "Failed to download "+file+": "+err.Error())
		return err
	}
	defer resp.Body.Close()

	// Create parent directories
	os.MkdirAll(filepath.Dir(file), 0755)

	// Create output file
	out, err := os.Create(file)
	if err != nil {
		E.Emit("error", "Failed to create file "+file+": "+err.Error())
		return err
	}
	defer out.Close()

	// Copy data from response body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		E.Emit("error", "Failed to write file "+file+": "+err.Error())
	} else {
		E.Emit("file_downloaded", file)
	}
	return err
}

// getOSName returns the Minecraft-specific operating system name based on runtime.GOOS.
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

// shouldIncludeLibrary determines if a library should be downloaded based on its OS rules.
func shouldIncludeLibrary(rules []struct {
	Action string `json:"action"`
	OS     struct {
		Name string `json:"name"`
	} `json:"os"`
}) bool {
	// If no rules, the library is always included.
	if len(rules) == 0 {
		return true
	}

	allowed := false
	osName := getOSName()

	for _, rule := range rules {
		if rule.Action == "allow" {
			// If rule applies to current OS (or no specific OS is defined)
			if rule.OS.Name == "" || rule.OS.Name == osName {
				allowed = true
			}
		} else if rule.Action == "disallow" {
			// If rule disallows for current OS (or no specific OS is defined), immediately exclude.
			if rule.OS.Name == "" || rule.OS.Name == osName {
				return false
			}
		}
	}
	// If there were rules, but none resulted in an explicit disallow, return if at least one allow rule matched.
	return allowed
}

// ------------------ Libraries ------------------

// DownloadLibraries iterates through the version metadata and downloads all necessary libraries,
// including main artifacts and OS-specific natives, applying OS rules.
func DownloadLibraries(metadata VersionMetadata, mcDir string, E *events.EventEmitter) {
	libDir := filepath.Join(mcDir, "libraries")
	osName := getOSName()

	for _, lib := range metadata.Libraries {
		// Check if library should be included based on rules
		if !shouldIncludeLibrary(lib.Rules) {
			E.Emit("library_skipped", lib.Name+" (OS rules)")
			continue
		}

		// Download main artifact (the primary JAR file)
		if lib.Downloads.Artifact.Url != "" && lib.Downloads.Artifact.Path != "" {
			url := lib.Downloads.Artifact.Url
			// Convert forward slashes in path to OS-specific path separators
			path := filepath.Join(libDir, filepath.FromSlash(lib.Downloads.Artifact.Path))

			E.Emit("library_download_start", lib.Name)
			if err := DownloadFile(path, url, E); err != nil {
				E.Emit("library_failed", lib.Name)
			} else {
				E.Emit("library_done", lib.Name)
			}
		}

		// Download natives (classifiers are typically native platform-specific libraries)
		if lib.Downloads.Classifiers != nil && len(lib.Downloads.Classifiers) > 0 {
			// Determine the native key string for this OS and architecture
			var nativeKey string
			if osName == "windows" {
				if runtime.GOARCH == "amd64" {
					nativeKey = "natives-windows"
				} else {
					nativeKey = "natives-windows-32" // Assuming x86 is 32-bit if not amd64
				}
			} else if osName == "osx" {
				nativeKey = "natives-osx"
			} else if osName == "linux" {
				nativeKey = "natives-linux"
			}

			// Download the matching native classifier
			for classifierName, classifier := range lib.Downloads.Classifiers {
				if strings.Contains(classifierName, nativeKey) || classifierName == nativeKey {
					if classifier.Url != "" && classifier.Path != "" {
						// Convert forward slashes in path to OS-specific path separators
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
			// Skip libraries that are neither an artifact nor a classifier (e.g., just for rules)
			E.Emit("library_skipped", lib.Name+" (no artifact URL)")
		}
	}
}

// ------------------ Assets ------------------

// DownloadAssets fetches the asset index and then downloads all required assets
// (textures, sounds, etc.) into the 'assets/objects' directory.
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

	// Iterate through all objects defined in the asset index
	for _, asset := range index.Objects {
		hash := asset.Hash
		// The path for assets is determined by the first two characters of the SHA1 hash
		sub := hash[:2]

		// Construct the final download URL and local path
		url := "https://resources.download.minecraft.net/" + sub + "/" + hash
		path := filepath.Join(objectsDir, sub, hash)

		E.Emit("asset_download_start", hash)
		_ = DownloadFile(path, url, E) // Ignore error to continue with next assets
	}

	E.Emit("assets_done", nil)
}

// ------------------ Version Download ------------------

// DownloadVersion orchestrates the entire download process for a vanilla Minecraft version,
// including fetching manifest, metadata, the client JAR, libraries, and assets.
func DownloadVersion(version string, mcDir string, E *events.EventEmitter) {
	E.Emit("version_download_start", version)

	// Fetch version manifest from Mojang
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

	// Find the specific version entry
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

	// Download detailed version metadata
	metaResp, err := http.Get(selected.Url)
	if err != nil {
		E.Emit("error", "Failed to fetch version metadata: "+err.Error())
		return
	}
	defer metaResp.Body.Close()

	metaBody, _ := io.ReadAll(metaResp.Body)
	var metadata VersionMetadata
	json.Unmarshal(metaBody, &metadata)

	// Download client jar and save metadata locally
	jarPath := filepath.Join(mcDir, "versions", version, version+".jar")
	metadataPath := filepath.Join(mcDir, "versions", version, version+".json")
	E.Emit("client_download_start", jarPath)
	_ = DownloadFile(jarPath, metadata.Downloads.Client.Url, E)

	// Save the metadata JSON file to the local version directory
	_ = os.WriteFile(metadataPath, metaBody, 0644)
	E.Emit("metadata_saved", metadataPath)

	// Download libraries (includes natives now!)
	DownloadLibraries(metadata, mcDir, E)

	// Download assets
	DownloadAssets(metadata, mcDir, E)

	E.Emit("version_downloaded", version)
}
