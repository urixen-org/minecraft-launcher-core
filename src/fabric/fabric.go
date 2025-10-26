package fabric

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/urixen-org/minecraft-launcher-core/src/downloader"
	"github.com/urixen-org/minecraft-launcher-core/src/events"
)

// ------------------ Metadata Structs ------------------

// FabricLoaderMetadata represents the structure of the Fabric version profile JSON
// downloaded from the Fabric meta-server, which is used to construct the custom
// version file for launching.
type FabricLoaderMetadata struct {
	MainClass string `json:"mainClass"`
	Libraries []struct {
		Name      string `json:"name"`
		Url       string `json:"url"` // Base URL for the library (often not used for Fabric libraries)
		Downloads struct {
			Artifact struct {
				Path string `json:"path"` // Relative path in the 'libraries' folder
				Url  string `json:"url"`  // Direct download URL for the artifact
			} `json:"artifact"`
			Classifiers map[string]struct {
				Path string `json:"path"`
				Url  string `json:"url"`
			} `json:"classifiers"`
		} `json:"downloads"`
	} `json:"libraries"`
	InheritsFrom string `json:"inheritsFrom"` // The base Minecraft version ID (e.g., "1.19.2")
	Id           string `json:"id"`           // The resulting version ID (e.g., "fabric-loader-0.14.9-1.19.2")
}

// ------------------ Download Loader Metadata ------------------

// fetchLoaderMeta downloads the Fabric version profile JSON for a specific
// Minecraft version and Fabric loader version.
func fetchLoaderMeta(mcVersion, loaderVersion string) (*FabricLoaderMetadata, error) {
	url := fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/profile/json", mcVersion, loaderVersion)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch Fabric metadata, status: %s", resp.Status)
	}

	var meta FabricLoaderMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}

	return &meta, nil
}

// ------------------ Library Download ------------------

// downloadFabricLibraries iterates through the required libraries in the Fabric metadata
// and downloads them into the Minecraft 'libraries' folder.
func downloadFabricLibraries(meta *FabricLoaderMetadata, mcDir string, E *events.EventEmitter) {
	libDir := filepath.Join(mcDir, "libraries")

	for _, lib := range meta.Libraries {
		// Download main artifact (the primary JAR)
		if lib.Downloads.Artifact.Url != "" && lib.Downloads.Artifact.Path != "" {
			path := filepath.Join(libDir, filepath.FromSlash(lib.Downloads.Artifact.Path))
			E.Emit("fabric_library_download_start", lib.Name)
			// downloader.DownloadFile handles creation of directories and checks for existence
			_ = downloader.DownloadFile(path, lib.Downloads.Artifact.Url, E)
		}

		// Download classifiers (e.g., natives or sources, though natives are less common for Fabric)
		for _, classifier := range lib.Downloads.Classifiers {
			if classifier.Url != "" && classifier.Path != "" {
				path := filepath.Join(libDir, filepath.FromSlash(classifier.Path))
				E.Emit("fabric_classifier_download_start", lib.Name)
				_ = downloader.DownloadFile(path, classifier.Url, E)
			}
		}
	}
}

// ------------------ Version JSON Builder ------------------

// buildFabricVersionJSON creates the final version JSON file required by the launcher
// in the appropriate 'versions' subdirectory.
func buildFabricVersionJSON(meta *FabricLoaderMetadata, mcDir, mcVersion string, E *events.EventEmitter) {
	// The new version ID includes the fabric loader version, e.g., "fabric-loader-0.14.9-1.19.2"
	versionDir := filepath.Join(mcDir, "versions", meta.Id)
	os.MkdirAll(versionDir, 0755)

	versionJsonPath := filepath.Join(versionDir, meta.Id+".json")

	// Write the downloaded and processed Fabric metadata as the new version file
	data, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(versionJsonPath, data, 0644)

	E.Emit("fabric_version_json_written", versionJsonPath)
}

// ------------------ Public API ------------------

// InstallFabric orchestrates the download and setup of Fabric Loader for a given
// Minecraft version and Fabric loader version.
// It ensures the base vanilla version is present, downloads Fabric libraries, and creates the launch JSON.
func InstallFabric(mcVersion, loaderVersion, mcDir string, E *events.EventEmitter) {
	E.Emit("fabric_install_start", mcVersion+" + loader "+loaderVersion)

	// 1. Get fabric metadata
	meta, err := fetchLoaderMeta(mcVersion, loaderVersion)
	if err != nil {
		E.Emit("error", "Failed to fetch Fabric metadata: "+err.Error())
		return
	}

	// 2. Ensure vanilla base version is installed first.
	// This makes sure the client JAR and assets are available before proceeding.
	downloader.DownloadVersion(mcVersion, mcDir, E)

	// 3. Download Fabric-specific libraries (including the loader JAR itself)
	downloadFabricLibraries(meta, mcDir, E)

	// 4. Write the merged version JSON for the launcher to read
	buildFabricVersionJSON(meta, mcDir, mcVersion, E)

	E.Emit("fabric_install_done", meta.Id)
}
