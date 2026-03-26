# CLAUDE.md

## Project Overview

macOS Cast Screen Cleaner — an interactive TUI tool that finds and removes cast screen app (MAXHUB/CVTE/UWST) remnants from macOS. Built with Go and the charmbracelet TUI stack (bubbletea, lipgloss, bubbles).

## Build & Run

```bash
go build -o cast-screen-cleaner .   # build
go run .                             # run without building
./cast-screen-cleaner                # run the binary (requires a real TTY)
```

The binary requires a real terminal (TTY) — it uses bubbletea's alt-screen mode and will fail with "could not open a new TTY" in non-interactive contexts.

## Project Structure

- `main.go` — Entry point. Creates and runs the bubbletea program.
- `scanner.go` — System scanning logic (finding remnants) and removal logic (deleting files, unloading services). Contains all types (`Category`, `CleanItem`, `RemoveResult`).
- `ui.go` — TUI model, state machine, key handling, and view rendering. Uses bubbletea's `Model` interface.

## Architecture

**State machine** (defined in `ui.go`): `stateScanning` → `stateSelecting` → `stateConfirming` → `stateSudo` (if needed) → `stateDone`. If nothing found, goes to `stateEmpty`.

**Scanner** (`scanner.go`): Scans known macOS directories for files matching the `keywords` slice (case-insensitive). Each scan function targets a specific category (audio drivers, launch agents, app data, etc.). Scans both user-level (`~/Library/...`) and system-level (`/Library/...`) paths, including nested paths like `~/Library/Caches/ScreenShare/bundle/` for screen recording cached apps.

**Removal flow**: User-level items are removed directly via `os.RemoveAll`. System-level items (NeedsSudo=true) are batched into a single bash script and run via `sudo` using `tea.ExecProcess` (which suspends the TUI for the password prompt). Services are unloaded via `launchctl unload` before file removal.

## Key Conventions

- Keywords for matching cast screen brands are in the `keywords` slice at the top of `scanner.go`. Add new brands there.
- Categories are ordered by the `Category` iota — the enum order determines display order in the TUI.
- `categoryMeta` slice must stay in sync with the `Category` const block (indexed by iota value).
- System-level paths (`/Library/...`) set `NeedsSudo: true`; user-level paths (`~/Library/...`) do not.
- Styles are defined as package-level `lipgloss.NewStyle()` vars at the top of `ui.go`.
- Viewport calculations in `ui.go` use `itemsFitFrom(start)` which walks items to compute exact line counts including category headers. The confirming and done views also support scrolling with ↑/↓ keys.
- To add new scan directories for app data, add entries to the `subdirs` (user-level) or `sysSubdirs` (system-level) slices in `scanAppData`.

## Dependencies

All from [charmbracelet](https://github.com/charmbracelet):
- `bubbletea` — TUI framework (Elm-architecture)
- `lipgloss` — Terminal styling
- `bubbles` — Reusable TUI components (spinner)
