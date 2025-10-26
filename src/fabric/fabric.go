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

type FabricLoaderMetadata struct {
	MainClass string `json:"mainClass"`
	Libraries []struct {
		Name      string `json:"name"`
		Url       string `json:"url"`
		Downloads struct {
			Artifact struct {
				Path string `json:"path"`
				Url  string `json:"url"`
			} `json:"artifact"`
			Classifiers map[string]struct {
				Path string `json:"path"`
				Url  string `json:"url"`
			} `json:"classifiers"`
		} `json:"downloads"`
	} `json:"libraries"`
	InheritsFrom string `json:"inheritsFrom"`
	Id           string `json:"id"`
}

// ------------------ Download Loader Metadata ------------------

func fetchLoaderMeta(mcVersion, loaderVersion string) (*FabricLoaderMetadata, error) {
	url := fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/profile/json", mcVersion, loaderVersion)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var meta FabricLoaderMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}

	return &meta, nil
}

// ------------------ Library Download ------------------

func downloadFabricLibraries(meta *FabricLoaderMetadata, mcDir string, E *events.EventEmitter) {
	libDir := filepath.Join(mcDir, "libraries")

	for _, lib := range meta.Libraries {
		// Download main artifact
		if lib.Downloads.Artifact.Url != "" && lib.Downloads.Artifact.Path != "" {
			path := filepath.Join(libDir, filepath.FromSlash(lib.Downloads.Artifact.Path))
			E.Emit("fabric_library_download_start", lib.Name)
			_ = downloader.DownloadFile(path, lib.Downloads.Artifact.Url, E)
		}

		// Download classifiers (e.g., natives)
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

func buildFabricVersionJSON(meta *FabricLoaderMetadata, mcDir, mcVersion string, E *events.EventEmitter) {
	versionDir := filepath.Join(mcDir, "versions", meta.Id)
	os.MkdirAll(versionDir, 0755)

	versionJsonPath := filepath.Join(versionDir, meta.Id+".json")

	// Write merged metadata to version file
	data, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(versionJsonPath, data, 0644)

	E.Emit("fabric_version_json_written", versionJsonPath)
}

// ------------------ Public API ------------------

func InstallFabric(mcVersion, loaderVersion, mcDir string, E *events.EventEmitter) {
	E.Emit("fabric_install_start", mcVersion+" + loader "+loaderVersion)

	// 1. Get fabric metadata
	meta, err := fetchLoaderMeta(mcVersion, loaderVersion)
	if err != nil {
		E.Emit("error", "Failed to fetch Fabric metadata: "+err.Error())
		return
	}

	// 2. Ensure vanilla base version installed first
	downloader.DownloadVersion(mcVersion, mcDir, E)

	// 3. Download Fabric libraries
	downloadFabricLibraries(meta, mcDir, E)

	// 4. Write merged version JSON for launcher
	buildFabricVersionJSON(meta, mcDir, mcVersion, E)

	E.Emit("fabric_install_done", meta.Id)
}
