# Cast Screen Cleaner for macOS

An interactive TUI tool to find and remove cast screen app (MAXHUB, CVTE, UWST, etc.) remnants from your Mac.

Cast screen apps used in meeting rooms often install system-level drivers, background services, and auto-startup agents that persist even after the app itself is removed. This tool scans your system, shows you exactly what was left behind, and lets you choose what to remove.

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-10.15+-000000?logo=apple&logoColor=white)
![License](https://img.shields.io/badge/License-MIT-blue)

## What It Detects

| Category | Scan Locations | Example |
|---|---|---|
| Audio Drivers | `/Library/Audio/Plug-Ins/HAL/` | `MAXHUBAudio.driver` |
| Video Drivers | `/Library/CoreMediaIO/Plug-Ins/DAL/` | Virtual camera plugins |
| Launch Daemons | `/Library/LaunchDaemons/` | System-level background services |
| Launch Agents | `/Library/LaunchAgents/`, `~/Library/LaunchAgents/` | `com.cvte.uwst.service.plist` |
| Application Data | `~/Library/UWST/`, `~/Library/Application Support/`, `~/Library/Caches/` | Cached apps, service data |
| Preferences | `~/Library/Preferences/`, `/Library/Preferences/` | `com.maxhub.dongleclient.plist` |
| Applications | `/Applications/` | App bundles |

## Installation

### Build from source

```bash
git clone https://github.com/lgiki/macOS-cast-screen-cleaner.git
cd macOS-cast-screen-cleaner
go build -o cast-screen-cleaner .
```

### Run directly

```bash
go run .
```

## Usage

```bash
./cast-screen-cleaner
```

The tool launches a full-screen interactive TUI:

1. **Scan** — Automatically scans your system for cast screen app remnants.
2. **Select** — Browse found items grouped by category. All items are selected by default. Toggle individual items on/off.
3. **Confirm** — Review what will be removed before proceeding.
4. **Remove** — User-level files are removed directly. System-level files prompt for your administrator password via `sudo`.
5. **Done** — See a per-item success/failure summary.

### Keyboard Controls

| Key | Action |
|---|---|
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `Space` | Toggle selection on current item |
| `a` | Select all items |
| `n` | Deselect all items |
| `Enter` | Confirm and proceed |
| `y` / `n` | Confirm or cancel on the confirmation screen |
| `q` / `Ctrl+C` | Quit |

### Indicators

- **sudo** — Item requires administrator privileges to remove.
- **● running** — A launch agent/daemon that is currently loaded and running. It will be unloaded before removal.

## Safety

- **User chooses what to remove** — Nothing is deleted without explicit selection and confirmation.
- **Two-step confirmation** — You review the full list of items before removal begins.
- **Services removed from launchd first** — Running launch agents/daemons are removed via `launchctl bootout` in the correct launchd domain before their plist files are deleted.
- **Minimal sudo scope** — Only system-level files (e.g., `/Library/` paths) require `sudo`. User-level files are removed directly.
- **Post-removal verification** — After `sudo` operations, the tool verifies whether each file was actually removed and reports any failures.

## Adding Support for Other Brands

The scanner matches files by keyword. To add support for another cast screen brand, add its identifiers to the `keywords` slice in [`scanner.go`](scanner.go):

```go
var keywords = []string{
    "maxhub", "cvte", "uwst",
    // Add new brands here, e.g.:
    // "bijie", "clickshare", "barco",
}
```

Then rebuild:

```bash
go build -o cast-screen-cleaner .
```

## Project Structure

```
.
├── main.go        # Entry point
├── scanner.go     # System scanning and removal logic
├── ui.go          # Interactive TUI (bubbletea model, views, key handling)
├── go.mod
└── go.sum
```

## Dependencies

- [bubbletea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [bubbles](https://github.com/charmbracelet/bubbles) — TUI components (spinner)

## License

MIT
