package launcher

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/urixen-org/minecraft-launcher-core/src/events"
)

// VersionJSON represents the version metadata
type VersionJSON struct {
	ID                     string `json:"id"`
	Type                   string `json:"type"`
	MainClass              string `json:"mainClass"`
	MinecraftArguments     string `json:"minecraftArguments"`
	InheritsFrom           string `json:"inheritsFrom"`
	MinimumLauncherVersion int    `json:"minimumLauncherVersion"`
	ReleaseTime            string `json:"releaseTime"`
	Time                   string `json:"time"`
	AssetIndex             struct {
		ID        string `json:"id"`
		SHA1      string `json:"sha1"`
		Size      int    `json:"size"`
		TotalSize int    `json:"totalSize"`
		URL       string `json:"url"`
	} `json:"assetIndex"`
	Assets    string `json:"assets"`
	Libraries []struct {
		Name      string `json:"name"`
		Downloads struct {
			Artifact struct {
				Path string `json:"path"`
				URL  string `json:"url"`
				SHA1 string `json:"sha1"`
				Size int    `json:"size"`
			} `json:"artifact"`
			Classifiers map[string]struct {
				Path string `json:"path"`
				URL  string `json:"url"`
				SHA1 string `json:"sha1"`
				Size int    `json:"size"`
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
	Arguments struct {
		Game []interface{} `json:"game"`
		JVM  []interface{} `json:"jvm"`
	} `json:"arguments"`
}

// extractJar extracts native files from a JAR to a destination directory
func extractJar(jarPath, destDir string, E *events.EventEmitter) error {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Skip directories
		if f.FileInfo().IsDir() {
			continue
		}

		// Skip META-INF
		if strings.HasPrefix(f.Name, "META-INF/") {
			continue
		}

		// Extract ALL files that look like natives (in any subdirectory)
		name := strings.ToLower(f.Name)
		isNative := strings.HasSuffix(name, ".dll") ||
			strings.HasSuffix(name, ".so") ||
			strings.HasSuffix(name, ".dylib") ||
			strings.HasSuffix(name, ".jnilib")

		if !isNative {
			continue
		}

		// Extract to flat directory structure
		destPath := filepath.Join(destDir, filepath.Base(f.Name))

		// Skip if already exists
		if _, err := os.Stat(destPath); err == nil {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}

		outFile, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			continue
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err == nil {
			E.Emit("native_extracted", filepath.Base(f.Name))
		}
	}

	return nil
}

// shouldIncludeLibrary checks if a library should be included based on rules
func shouldIncludeLibrary(rules []struct {
	Action string `json:"action"`
	OS     struct {
		Name string `json:"name"`
	} `json:"os"`
}) bool {
	if len(rules) == 0 {
		return true
	}

	osName := getOSName()
	allowed := false

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

// extractNativesFromLibraries extracts natives from all JARs in libraries
func extractNativesFromLibraries(libDir, nativesDir string, E *events.EventEmitter) error {
	if err := os.MkdirAll(nativesDir, 0o755); err != nil {
		return err
	}

	// Check if natives already extracted
	entries, err := os.ReadDir(nativesDir)
	if err == nil && len(entries) > 0 {
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			if strings.HasSuffix(name, ".dll") || strings.HasSuffix(name, ".so") ||
				strings.HasSuffix(name, ".dylib") || strings.HasSuffix(name, ".jnilib") {
				E.Emit("natives_already_extracted", nativesDir)
				return nil
			}
		}
	}

	E.Emit("extracting_natives_start", libDir)

	// Platform detection
	var nativePattern string
	switch runtime.GOOS {
	case "windows":
		nativePattern = "natives-windows"
	case "darwin":
		nativePattern = "natives-osx"
	case "linux":
		nativePattern = "natives-linux"
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// Extract from all JARs (walk recursively through subdirectories)
	filepath.Walk(libDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(info.Name(), ".jar") {
			return nil
		}

		lowerName := strings.ToLower(info.Name())

		// Check if this JAR contains natives for our platform
		if strings.Contains(lowerName, nativePattern) || strings.Contains(lowerName, "natives") {
			E.Emit("native_jar_processing", info.Name())
			extractJar(path, nativesDir, E)
		}

		return nil
	})

	// Verify extraction
	entries, err = os.ReadDir(nativesDir)
	if err != nil {
		return fmt.Errorf("failed to read natives directory: %w", err)
	}

	nativeCount := 0
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".dll") || strings.HasSuffix(name, ".so") ||
			strings.HasSuffix(name, ".dylib") || strings.HasSuffix(name, ".jnilib") {
			nativeCount++
		}
	}

	if nativeCount == 0 {
		E.Emit("error", "No native libraries were extracted - check if native JARs exist in libraries")
		return fmt.Errorf("no native libraries were extracted - check if native JARs exist in libraries")
	}

	E.Emit("natives_extracted", nativeCount)
	return nil
}

// loadVersionJSON loads and parses the version JSON file
func loadVersionJSON(gameDir, version string, E *events.EventEmitter) (*VersionJSON, error) {
	versionJSONPath := filepath.Join(gameDir, "versions", version, version+".json")

	data, err := os.ReadFile(versionJSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read version JSON: %w", err)
	}

	var versionJSON VersionJSON
	if err := json.Unmarshal(data, &versionJSON); err != nil {
		return nil, fmt.Errorf("failed to parse version JSON: %w", err)
	}

	// Check if this version inherits from another (Optifine, Forge, Fabric, etc.)
	if versionJSON.InheritsFrom != "" {
		E.Emit("version_inherits_from", versionJSON.InheritsFrom)

		// Load parent version
		parentJSON, err := loadVersionJSON(gameDir, versionJSON.InheritsFrom, E)
		if err != nil {
			return nil, fmt.Errorf("failed to load parent version %s: %w", versionJSON.InheritsFrom, err)
		}

		// Merge: child overrides parent
		if versionJSON.MainClass == "" {
			versionJSON.MainClass = parentJSON.MainClass
		}
		if versionJSON.MinecraftArguments == "" {
			versionJSON.MinecraftArguments = parentJSON.MinecraftArguments
		}
		if versionJSON.AssetIndex.ID == "" {
			versionJSON.AssetIndex = parentJSON.AssetIndex
		}
		if versionJSON.Assets == "" {
			versionJSON.Assets = parentJSON.Assets
		}

		// Merge libraries: parent first, then child (child can override)
		mergedLibs := append([]struct {
			Name      string `json:"name"`
			Downloads struct {
				Artifact struct {
					Path string `json:"path"`
					URL  string `json:"url"`
					SHA1 string `json:"sha1"`
					Size int    `json:"size"`
				} `json:"artifact"`
				Classifiers map[string]struct {
					Path string `json:"path"`
					URL  string `json:"url"`
					SHA1 string `json:"sha1"`
					Size int    `json:"size"`
				} `json:"classifiers"`
			} `json:"downloads"`
			Rules []struct {
				Action string `json:"action"`
				OS     struct {
					Name string `json:"name"`
				} `json:"os"`
			} `json:"rules"`
			Natives map[string]string `json:"natives"`
		}{}, parentJSON.Libraries...)
		mergedLibs = append(mergedLibs, versionJSON.Libraries...)
		versionJSON.Libraries = mergedLibs

		E.Emit("version_merged", map[string]string{
			"child":  version,
			"parent": versionJSON.InheritsFrom,
		})
	}

	return &versionJSON, nil
}

// parseMinecraftArguments parses the minecraftArguments string and replaces placeholders
func parseMinecraftArguments(template string, replacements map[string]string) []string {
	// Replace all placeholders
	for key, value := range replacements {
		template = strings.ReplaceAll(template, "${"+key+"}", value)
	}

	// Split into arguments
	args := strings.Fields(template)
	return args
}

// buildClasspath builds the classpath from libraries
func buildClasspath(gameDir, version string, versionJSON *VersionJSON, E *events.EventEmitter) string {
	libDir := filepath.Join(gameDir, "libraries")
	versionDir := filepath.Join(gameDir, "versions", version)
	var classpathParts []string

	// Add all libraries that match the current OS
	for _, lib := range versionJSON.Libraries {
		if !shouldIncludeLibrary(lib.Rules) {
			continue
		}

		if lib.Downloads.Artifact.Path != "" {
			libPath := filepath.Join(libDir, filepath.FromSlash(lib.Downloads.Artifact.Path))
			if _, err := os.Stat(libPath); err == nil {
				classpathParts = append(classpathParts, libPath)
			} else {
				E.Emit("library_missing", map[string]string{
					"name": lib.Name,
					"path": libPath,
				})
			}
		} else if lib.Name != "" {
			// Libraries without download info (Optifine, launchwrapper, etc.)
			// These are usually in the version folder

			// Parse library name: group:artifact:version
			parts := strings.Split(lib.Name, ":")
			if len(parts) >= 3 {
				group := parts[0]
				artifact := parts[1]
				version := parts[2]

				// Try common patterns for modded libraries
				possiblePaths := []string{
					// Pattern 1: versions/1.8.9-OptiFine_HD_U_M5/1.8.9-OptiFine_HD_U_M5.jar (Optifine itself)
					filepath.Join(versionDir, artifact+"-"+version+".jar"),
					// Pattern 2: libraries/group/artifact/version/artifact-version.jar
					filepath.Join(libDir, filepath.FromSlash(group), artifact, version, artifact+"-"+version+".jar"),
					// Pattern 3: libraries/group/artifact/artifact-version.jar
					filepath.Join(libDir, filepath.FromSlash(group), artifact, artifact+"-"+version+".jar"),
					// Pattern 4: Check if it's in version folder with full name
					filepath.Join(versionDir, lib.Name+".jar"),
				}

				found := false
				for _, path := range possiblePaths {
					if _, err := os.Stat(path); err == nil {
						classpathParts = append(classpathParts, path)
						E.Emit("library_found_alternative", map[string]string{
							"name": lib.Name,
							"path": path,
						})
						found = true
						break
					}
				}

				if !found {
					E.Emit("library_not_found", lib.Name)
				}
			}
		}
	}

	// Add version JAR
	versionJar := filepath.Join(versionDir, version+".jar")
	if _, err := os.Stat(versionJar); err == nil {
		classpathParts = append(classpathParts, versionJar)
	}

	E.Emit("classpath_built", len(classpathParts))
	return strings.Join(classpathParts, string(os.PathListSeparator))
}

// PrepareCMD builds a Java command to launch Minecraft
func PrepareCMD(
	username, accessToken, uuid, gameDir, version, javaPath, maxRam, minRam string,
	E *events.EventEmitter,
) (string, []string, error) {
	if username == "" {
		username = "Player"
	}
	if javaPath == "" {
		javaPath = "java"
	}
	if maxRam == "" {
		maxRam = "2G"
	}
	if minRam == "" {
		minRam = "512M"
	}
	if accessToken == "" {
		accessToken = "0"
	}
	if uuid == "" {
		uuid = "00000000-0000-0000-0000-000000000000"
	}

	E.Emit("launch_preparation_start", version)

	// Load version JSON
	versionJSON, err := loadVersionJSON(gameDir, version, E)
	if err != nil {
		E.Emit("error", err.Error())
		return "", nil, err
	}

	E.Emit("version_json_loaded", versionJSON.ID)

	versionDir := filepath.Join(gameDir, "versions", version)
	versionJar := filepath.Join(versionDir, version+".jar")

	// For modded versions (Fabric, Forge, etc.), use the parent JAR if child JAR doesn't exist
	if _, err := os.Stat(versionJar); os.IsNotExist(err) {
		if versionJSON.InheritsFrom != "" {
			parentJar := filepath.Join(gameDir, "versions", versionJSON.InheritsFrom, versionJSON.InheritsFrom+".jar")
			if _, err := os.Stat(parentJar); err == nil {
				E.Emit("using_parent_jar", versionJSON.InheritsFrom)
				versionJar = parentJar
			} else {
				E.Emit("error", "Neither version jar nor parent jar found")
				return "", nil, fmt.Errorf("version jar not found: %s and parent jar not found: %s", versionJar, parentJar)
			}
		} else {
			E.Emit("error", "Version jar not found: "+versionJar)
			return "", nil, fmt.Errorf("version jar not found: %s", versionJar)
		}
	}

	// Extract natives
	nativesDir := filepath.Join(versionDir, "natives")
	libDir := filepath.Join(gameDir, "libraries")

	if err := extractNativesFromLibraries(libDir, nativesDir, E); err != nil {
		E.Emit("error", "Failed to extract natives: "+err.Error())
		return "", nil, fmt.Errorf("failed to extract natives: %w", err)
	}

	// Build classpath
	E.Emit("building_classpath", libDir)
	classpath := buildClasspath(gameDir, version, versionJSON, E)

	// Absolute path for natives
	absNativesDir, _ := filepath.Abs(nativesDir)

	// Determine asset index
	assetIndex := versionJSON.AssetIndex.ID
	if versionJSON.Assets != "" {
		assetIndex = versionJSON.Assets
	}

	// Build JVM arguments
	args := []string{
		"-Xmx" + maxRam,
		"-Xms" + minRam,
		"-Djava.library.path=" + absNativesDir,
		"-cp", classpath,
	}

	// Add main class from version JSON
	mainClass := versionJSON.MainClass
	if mainClass == "" {
		mainClass = "net.minecraft.client.main.Main" // fallback
	}
	args = append(args, mainClass)

	// Parse and add game arguments from version JSON
	if versionJSON.MinecraftArguments != "" {
		// Old format (1.12.2 and below)
		replacements := map[string]string{
			"auth_player_name":  username,
			"version_name":      version,
			"game_directory":    gameDir,
			"assets_root":       filepath.Join(gameDir, "assets"),
			"assets_index_name": assetIndex,
			"auth_uuid":         uuid,
			"auth_access_token": accessToken,
			"user_properties":   "{}",
			"user_type":         "legacy",
		}
		gameArgs := parseMinecraftArguments(versionJSON.MinecraftArguments, replacements)
		args = append(args, gameArgs...)
	} else if len(versionJSON.Arguments.Game) > 0 {
		// New format (1.13+)
		// TODO: Implement new argument format parsing
		// For now, fall back to manual arguments
		gameArgs := []string{
			"--username", username,
			"--version", version,
			"--gameDir", gameDir,
			"--assetsDir", filepath.Join(gameDir, "assets"),
			"--assetIndex", assetIndex,
			"--uuid", uuid,
			"--accessToken", accessToken,
			"--userType", "legacy",
		}
		args = append(args, gameArgs...)
	} else {
		// Manual fallback
		gameArgs := []string{
			"--username", username,
			"--version", version,
			"--gameDir", gameDir,
			"--assetsDir", filepath.Join(gameDir, "assets"),
			"--assetIndex", assetIndex,
			"--uuid", uuid,
			"--accessToken", accessToken,
			"--userType", "legacy",
		}
		args = append(args, gameArgs...)
	}

	E.Emit("launch_preparation_complete", map[string]interface{}{
		"username":  username,
		"version":   version,
		"javaPath":  javaPath,
		"mainClass": mainClass,
	})

	return javaPath, args, nil
}

// LaunchMinecraft launches Minecraft with the given parameters
func LaunchMinecraft(username, accessToken, uuid, gameDir, version, javaPath, maxRam, minRam string, E *events.EventEmitter) (*exec.Cmd, error) {
	javaPath, args, err := PrepareCMD(username, accessToken, uuid, gameDir, version, javaPath, maxRam, minRam, E)
	if err != nil {
		return nil, err
	}

	E.Emit("launching_game", version)

	cmd := exec.Command(javaPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd, nil
}
