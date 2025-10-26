# ⛏️ Minecraft Launcher Core (Go)

A **high-performance, modular, and thread-safe Go library** designed to abstract the complex logistics of downloading, managing, and launching various versions of the Minecraft client (Mojang API, libraries, assets, and mod loaders).

This core is engineered for reliability and concurrency, providing a robust foundation for custom launcher applications.

---

## 🎯 Architecture and Packages

The library follows the **Single Responsibility Principle**, dividing its logic into four distinct, highly cohesive packages:

| Package | Responsibility | Key Exported Functions | Design Focus |
| :--- | :--- | :--- | :--- |
| **`events`** | **Asynchronous Communication** | `New()`, `On()`, `Emit()` | Thread-safe, minimal overhead event signaling. |
| **`downloader`** | **Vanilla Artifact Management** | `DownloadVersion()`, `DownloadFile()` | Handles manifest parsing, URL generation, and I/O operations for Mojang endpoints. |
| **`fabric`** | **Mod Loader Integration** | `InstallFabric()` | Orchestrates metadata retrieval and library installation for Fabric modded versions. |
| **`launcher`** | **Command Preparation & Execution** | `PrepareCMD()`, `LaunchMinecraft()`, `buildClasspath()` | Manages version profiles, native extraction, argument substitution, and Java command construction. |

---

## ⚙️ Core Logic Flow

The launch process involves two main phases: **Installation** (idempotent setup) and **Execution** (runtime command generation).

### Phase 1: Installation (`downloader` / `fabric`)

This phase ensures all required files exist in the correct locations within the `.minecraft` directory (`mcDir`).

1.  **Version Profile:** Fetch the primary version manifest and the target version's detailed JSON.
2.  **Inheritance Handling:** If a modded version is requested (`fabric`), the base vanilla version is installed first.
3.  **Artifact Download:** Recursively fetch the client JAR, all required libraries, and all game assets (textures, sounds, etc.).
4.  **Local Metadata:** The final, **merged launch JSON** (including mod loader libraries and main class overrides) is saved to the local `versions/<ID>/<ID>.json` file.

### Phase 2: Execution (`launcher`)

This phase builds the exact command required to start the JVM.

1.  **Load & Merge:** `launcher.loadVersionJSON` reads the final local JSON, ensuring fields (like `mainClass` and `libraries`) are correctly combined if inheritance was used (e.g., Fabric over vanilla).
2.  **Native Extraction:** `launcher.extractNativesFromLibraries` unzips all platform-specific dynamic libraries (`.dll`, `.so`, `.dylib`) from the downloaded JARs into a temporary `natives` folder.
3.  **Classpath Construction:** `launcher.buildClasspath` creates the full, system-separated string of every dependency JAR required by the JVM.
4.  **Argument Substitution:** The function `launcher.PrepareCMD` handles token replacement for both **old** (`minecraftArguments`) and **new** (`arguments` object) argument formats, substituting variables like `${auth_player_name}` and `${game_directory}`.
5.  **Command Finalization:** A complete `java -Xmx... -Djava.library.path=... -cp ... MainClass --gameDir ...` command is returned as a ready-to-run `*exec.Cmd`.

---

## 📢 Eventing System (`events`)

The **`EventEmitter`** provides a clean, non-blocking interface for the core logic to communicate status and errors back to the UI or logging layer.

| Event Name | Purpose | Example Data (type) | Origin |
| :--- | :--- | :--- | :--- |
| `file_downloaded` | Successful download of any artifact. | `/path/to/file.jar` (`string`) | `downloader` |
| `library_missing` | Required dependency was not found locally. | `{name: "guava", path: "..."}` (`map`) | `launcher` |
| `natives_extracted` | Native extraction complete and verified. | `12` (count) (`int`) | `launcher` |
| `version_merged` | Confirms successful merging of parent/child profiles. | `{child: "fabric-...", parent: "1.20.1"}` (`map`) | `launcher` |
| `error` | Reports any non-recoverable error. | `Failed to fetch manifest: EOF` (`string`) | All |