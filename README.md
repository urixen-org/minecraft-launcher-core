# Minecraft Launcher Core (Go)

A **high-performance, modular, and thread-safe Go library** designed to abstract the complex logistics of downloading, managing, and launching various versions of the Minecraft client (Mojang API, libraries, assets, and mod loaders).

This core is engineered for reliability and concurrency, providing a robust foundation for custom launcher applications.

---

## 🎯 Architecture and Packages

The library follows the **Single Responsibility Principle**, dividing its logic into distinct, highly cohesive packages:

| Package | Responsibility | Key Exported Functions | Design Focus |
| :--- | :--- | :--- | :--- |
| **`events`** | **Asynchronous Communication** | `New()`, `On()`, `Emit()` | Thread-safe, minimal overhead event signaling. |
| **`downloader`** | **Vanilla Artifact Management** | `DownloadVersion()`, `DownloadFile()` | Handles manifest parsing, URL generation, and I/O operations for Mojang endpoints. |
| **`fabric`** | **Mod Loader Integration** | `InstallFabric()` | Orchestrates metadata retrieval and library installation for Fabric modded versions. |
| **`launcher`** | **Command Preparation & Execution** | `PrepareCMD()`, `LaunchMinecraft()`, `buildClasspath()` | Manages version profiles, native extraction, argument substitution, and JVM command construction. |
| **`utils`** | **General Launcher Utilities** | `GetMCDir()`, `SetMCDir()`, `GetAllVanillaMCVersions()` | Provides file handling, version fetching, downloads, and backups. |

> You can add more packages here, e.g., `forge` for Forge mod support or `server` for lightweight launcher-side server management.

---

## 💡 Core Logic Flow

The launch process is split into **Installation** and **Execution** phases.

### Phase 1: Installation (`downloader` / `fabric`)

Ensures all required files exist in the correct `.minecraft` structure:

1. **Version Profile:** Fetch the primary version manifest and the target version’s JSON.  
2. **Inheritance Handling:** For modded versions (Fabric), ensure base vanilla version is installed first.  
3. **Artifact Download:** Recursively fetch client JAR, required libraries, and assets.  
4. **Local Metadata:** Save a merged launch JSON (`versions/<ID>/<ID>.json`) with all mod loader overrides applied.  

### Phase 2: Execution (`launcher`)

Constructs the final Java command to launch Minecraft:

1. **Load & Merge:** Read local JSON, ensure fields (`mainClass`, `libraries`) are merged if needed.  
2. **Native Extraction:** Extract platform-specific libraries (`.dll`, `.so`, `.dylib`) into a temporary `natives` folder.  
3. **Classpath Construction:** Combine every library into a JVM-compatible classpath string.  
4. **Argument Substitution:** Replace variables (`${auth_player_name}`, `${game_directory}`, etc.) in old/new argument formats.  
5. **Command Finalization:** Return a fully-prepared `*exec.Cmd` ready to launch Minecraft.

---

## ⚡ Event System (`events`)

`EventEmitter` provides a **non-blocking interface** to communicate status and errors to the UI or logs:

| Event Name | Purpose | Example Data (type) | Origin |
| :--- | :--- | :--- | :--- |
| `file_downloaded` | A file or library was successfully downloaded. | `/path/to/file.jar` (`string`) | `downloader` |
| `library_missing` | A required dependency was not found locally. | `{name: "guava", path: "..."}` (`map`) | `launcher` |
| `natives_extracted` | Natives were extracted and verified. | `12` (`int`) | `launcher` |
| `version_merged` | Confirms parent/child JSON merging. | `{child: "fabric-1.20.1", parent: "1.20.1"}` (`map`) | `launcher` |
| `error` | Reports unrecoverable errors. | `Failed to fetch manifest: EOF` (`string`) | All |

> You can extend the event system with custom events for mod downloads, game logging, or UI updates.

---

## 🔧 Utilities (`utils`)

Utility functions for the launcher:

- `GetMCDir()` / `SetMCDir(dir string)` – Get/set custom Minecraft directory.  
- `GetAllVanillaMCVersions()` – Fetch all official Mojang versions.  
- `GetLatestMCVersion()` – Fetch the latest release.  
- `DownloadFile(url, dest)` – Download any file from the web.  
- `BackupFile(src, backup)` – Backup files safely.  

> This package is fully launcher-independent and can be extended to handle assets, configs, or lightweight server info.

---

## 📦 Extending the Core

You can extend the launcher core in multiple ways:

1. **Add Mod Loader Packages**  
   - `forge`, `quilt`, etc.  
2. **Add Logging/Telemetry**  
   - Emit events for detailed runtime monitoring.  
3. **Integrate Custom UI**  
   - Wrap `PrepareCMD()` and event handling for GUI launchers.  
4. **Asset Management**  
   - Preload textures, sounds, or mod files efficiently.  
5. **Server Integration (optional)**  
   - Track saved servers or allow lightweight server launches from the launcher.  

> Keep each package **single-responsibility**, thread-safe, and event-driven for maximum modularity.

---

## ⚖️ License

## MIT license

---

## 📝 Notes

- All network operations are **thread-safe**.  
- The library avoids writing anything to the user’s `.minecraft` folder unless explicitly requested.  
- `utils` and `downloader` are fully decoupled and can be reused in other Go projects.  
