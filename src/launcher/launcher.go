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

// VersionJSON represents the structure of the Minecraft version metadata JSON file.
// This file contains all necessary information to launch a specific version, including libraries and arguments.
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

// extractJar extracts native files (DLL, SO, DYLIB, JNILIB) from a JAR archive
// into a flat destination directory. It skips files in META-INF/.
func extractJar(jarPath, destDir string, E *events.EventEmitter) error {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Skip directories and META-INF
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "META-INF/") {
			continue
		}

		// Check if the file is a native library based on its extension
		name := strings.ToLower(f.Name)
		isNative := strings.HasSuffix(name, ".dll") ||
			strings.HasSuffix(name, ".so") ||
			strings.HasSuffix(name, ".dylib") ||
			strings.HasSuffix(name, ".jnilib")

		if !isNative {
			continue
		}

		// Extract to a flat directory structure (using only the filename)
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

// shouldIncludeLibrary checks if a library should be included based on its OS rules defined in the version JSON.
func shouldIncludeLibrary(rules []struct {
	Action string `json:"action"`
	OS     struct {
		Name string `json:"name"`
	} `json:"os"`
}) bool {
	// If no rules are defined, the library is always included.
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
				return false // Disallow rules are absolute
			}
		}
	}
	// If there were rules, but none disallowed it, return true only if an allow rule matched.
	return allowed
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

// extractNativesFromLibraries recursively walks the libraries directory, identifies platform-specific
// native JARs, and extracts their contents into the version's natives directory.
func extractNativesFromLibraries(libDir, nativesDir string, E *events.EventEmitter) error {
	if err := os.MkdirAll(nativesDir, 0o755); err != nil {
		return err
	}

	// Check for existing natives to skip extraction if already done
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

	// Determine the platform pattern to match native JAR filenames
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

	// Walk recursively and extract from matching JARs
	filepath.Walk(libDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".jar") {
			return nil
		}

		lowerName := strings.ToLower(info.Name())

		// A JAR is considered a native JAR if it contains the platform-specific pattern or "natives"
		if strings.Contains(lowerName, nativePattern) || strings.Contains(lowerName, "natives") {
			E.Emit("native_jar_processing", info.Name())
			// Ignore error from extractJar to continue processing other libraries
			extractJar(path, nativesDir, E)
		}

		return nil
	})

	// Verify that at least one native file was extracted
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

// loadVersionJSON loads, parses, and handles version inheritance for a specific version JSON file.
// If the version inherits from a parent, their fields are merged (child overrides parent).
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

	// Handle version inheritance (common for mod loaders like Forge, Fabric, OptiFine)
	if versionJSON.InheritsFrom != "" {
		E.Emit("version_inherits_from", versionJSON.InheritsFrom)

		// Load the parent version's JSON
		parentJSON, err := loadVersionJSON(gameDir, versionJSON.InheritsFrom, E)
		if err != nil {
			return nil, fmt.Errorf("failed to load parent version %s: %w", versionJSON.InheritsFrom, err)
		}

		// Merge fields: if the child's field is empty, inherit the parent's value
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

		// Merge libraries: Parent libraries come first, followed by child libraries.
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

// parseMinecraftArguments replaces placeholders in the `minecraftArguments` template string
// with actual values and splits the result into a slice of command-line arguments.
func parseMinecraftArguments(template string, replacements map[string]string) []string {
	// Replace all placeholders like ${auth_player_name}
	for key, value := range replacements {
		template = strings.ReplaceAll(template, "${"+key+"}", value)
	}

	// Split the resulting string into arguments based on whitespace
	args := strings.Fields(template)
	return args
}

// buildClasspath constructs the Java classpath string by finding the absolute paths
// of all required and downloaded libraries, separated by the system's path list separator.
func buildClasspath(gameDir, version string, versionJSON *VersionJSON, E *events.EventEmitter) string {
	libDir := filepath.Join(gameDir, "libraries")
	versionDir := filepath.Join(gameDir, "versions", version)
	var classpathParts []string

	// Add all required libraries (checking OS rules)
	for _, lib := range versionJSON.Libraries {
		if !shouldIncludeLibrary(lib.Rules) {
			continue
		}

		if lib.Downloads.Artifact.Path != "" {
			// Library with a defined artifact path (vanilla)
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
			// Library without a download path (often used for modded launchers like Forge/Fabric)
			// It requires checking alternative, non-standard path patterns.
			parts := strings.Split(lib.Name, ":")
			if len(parts) >= 3 {
				group := parts[0]
				artifact := parts[1]
				version := parts[2]

				// Check common paths for modded libraries
				possiblePaths := []string{
					// Pattern 1: `versionDir/artifact-version.jar` (e.g., Optifine or main mod loader JAR)
					filepath.Join(versionDir, artifact+"-"+version+".jar"),
					// Pattern 2: `libraries/group/artifact/version/artifact-version.jar` (Maven standard)
					filepath.Join(libDir, filepath.FromSlash(group), artifact, version, artifact+"-"+version+".jar"),
					// Pattern 3: `libraries/group/artifact/artifact-version.jar` (Less common variation)
					filepath.Join(libDir, filepath.FromSlash(group), artifact, artifact+"-"+version+".jar"),
					// Pattern 4: `versionDir/lib.Name.jar`
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

	// Add the main version JAR to the classpath last
	versionJar := filepath.Join(versionDir, version+".jar")
	if _, err := os.Stat(versionJar); err == nil {
		classpathParts = append(classpathParts, versionJar)
	}

	E.Emit("classpath_built", len(classpathParts))
	// Join all parts with the OS-specific path list separator (e.g., ':' on Linux, ';' on Windows)
	return strings.Join(classpathParts, string(os.PathListSeparator))
}

// PrepareCMD prepares the Java executable path and command-line arguments required to launch Minecraft.
// It handles argument construction, memory settings, and finding the main class.
func PrepareCMD(
	username, accessToken, uuid, gameDir, version, javaPath, maxRam, minRam string,
	E *events.EventEmitter,
) (string, []string, error) {
	// Apply default values if not provided
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
		accessToken = "0" // Placeholder for offline mode
	}
	if uuid == "" {
		uuid = "00000000-0000-0000-0000-000000000000"
	}

	E.Emit("launch_preparation_start", version)

	// Load and merge version JSON
	versionJSON, err := loadVersionJSON(gameDir, version, E)
	if err != nil {
		E.Emit("error", err.Error())
		return "", nil, err
	}

	E.Emit("version_json_loaded", versionJSON.ID)

	versionDir := filepath.Join(gameDir, "versions", version)
	versionJar := filepath.Join(versionDir, version+".jar")

	// Check for the client JAR. If a modded version is used, fall back to the parent version's JAR.
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

	// Extract natives from libraries
	nativesDir := filepath.Join(versionDir, "natives")
	libDir := filepath.Join(gameDir, "libraries")

	if err := extractNativesFromLibraries(libDir, nativesDir, E); err != nil {
		E.Emit("error", "Failed to extract natives: "+err.Error())
		return "", nil, fmt.Errorf("failed to extract natives: %w", err)
	}

	// Build the classpath
	E.Emit("building_classpath", libDir)
	classpath := buildClasspath(gameDir, version, versionJSON, E)

	// Get absolute path for natives directory, required for Java library path
	absNativesDir, _ := filepath.Abs(nativesDir)

	// Determine the asset index name
	assetIndex := versionJSON.AssetIndex.ID
	if versionJSON.Assets != "" {
		assetIndex = versionJSON.Assets
	}

	// Build base JVM arguments
	args := []string{
		"-Xmx" + maxRam,                        // Maximum memory allocation
		"-Xms" + minRam,                        // Initial memory allocation
		"-Djava.library.path=" + absNativesDir, // Path to extracted native libraries
		"-cp", classpath,                       // The constructed classpath
	}

	// Append main class
	mainClass := versionJSON.MainClass
	if mainClass == "" {
		mainClass = "net.minecraft.client.main.Main" // Vanilla fallback
	}
	args = append(args, mainClass)

	// Parse and append game arguments
	if versionJSON.MinecraftArguments != "" {
		// Old argument format (pre-1.13)
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
		// New argument format (1.13+) - Full implementation is complex; a manual fallback is used.
		// NOTE: The full logic for the new format (1.13+) including rules for Game and JVM arguments is complex
		// and is not fully implemented in the provided code (marked with TODO). The following is a manual fallback.
		fallBackArgs := []string{
			"--username", username,
			"--version", version,
			"--gameDir", gameDir,
			"--assetsDir", filepath.Join(gameDir, "assets"),
			"--assetIndex", assetIndex,
			"--uuid", uuid,
			"--accessToken", accessToken,
			"--userType", "legacy",
		}
		args = append(args, fallBackArgs...)
	} else {
		// Generic manual fallback for any unparsed or missing argument format
		fallBackArgs := []string{
			"--username", username,
			"--version", version,
			"--gameDir", gameDir,
			"--assetsDir", filepath.Join(gameDir, "assets"),
			"--assetIndex", assetIndex,
			"--uuid", uuid,
			"--accessToken", accessToken,
			"--userType", "legacy",
		}
		args = append(args, fallBackArgs...)
	}

	E.Emit("launch_preparation_complete", map[string]interface{}{
		"username":  username,
		"version":   version,
		"javaPath":  javaPath,
		"mainClass": mainClass,
	})

	return javaPath, args, nil
}

// LaunchMinecraft prepares the Java command and returns an *exec.Cmd ready to be started.
func LaunchMinecraft(username, accessToken, uuid, gameDir, version, javaPath, maxRam, minRam string, E *events.EventEmitter) (*exec.Cmd, error) {
	// Get the executable path and arguments
	javaPath, args, err := PrepareCMD(username, accessToken, uuid, gameDir, version, javaPath, maxRam, minRam, E)
	if err != nil {
		return nil, err
	}

	E.Emit("launching_game", version)

	// Create the command object
	cmd := exec.Command(javaPath, args...)
	// Direct the child process's I/O to the launcher's I/O
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd, nil
}
