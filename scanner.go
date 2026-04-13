package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Category represents the type of a discovered item.
type Category int

const (
	CatApplication Category = iota
	CatAudioDriver
	CatVideoDriver
	CatLaunchDaemon
	CatLaunchAgent
	CatAppData
	CatPreference
)

// CategoryMeta holds display metadata for each category.
type CategoryMeta struct {
	Name string
	Icon string
}

// categoryMeta is indexed by Category iota values.
var categoryMeta = []CategoryMeta{
	{"Applications", "◆"},
	{"Audio Drivers", "♫"},
	{"Video Drivers", "▶"},
	{"Launch Daemons", "⚙"},
	{"Launch Agents", "⚡"},
	{"Application Data", "▪"},
	{"Preferences", "○"},
}

// CleanItem represents a single removable item found on the system.
type CleanItem struct {
	Category       Category
	Path           string
	Name           string
	Description    string
	NeedsSudo      bool
	IsService      bool
	ServiceLabel   string
	ServiceDomains []string
	IsLoaded       bool
	Size           int64
}

// SizeStr returns a human-readable size string.
func (c CleanItem) SizeStr() string {
	return formatSize(c.Size)
}

// ShortPath returns the path with the home directory abbreviated to ~.
func (c CleanItem) ShortPath() string {
	return shortenHome(c.Path)
}

// RemoveResult holds the outcome of removing a single item.
type RemoveResult struct {
	Item    CleanItem
	Success bool
	Error   string
}

// ---------------------------------------------------------------------------
// Keywords — add new cast-screen brands here
// ---------------------------------------------------------------------------

var keywords = []string{
	"maxhub", "cvte", "uwst", "andisplay", "bozee",
}

func containsKeyword(s string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var (
	_homeDir     string
	_homeDirOnce sync.Once
)

func homeDir() string {
	_homeDirOnce.Do(func() {
		h, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
			os.Exit(1)
		}
		_homeDir = h
	})
	return _homeDir
}

func shortenHome(p string) string {
	home := homeDir()
	if strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func calcSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

func formatSize(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.1f GB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func serviceDomains(cat Category) []string {
	switch cat {
	case CatLaunchDaemon:
		return []string{"system"}
	case CatLaunchAgent:
		uid := os.Getuid()
		return []string{
			fmt.Sprintf("gui/%d", uid),
			fmt.Sprintf("user/%d", uid),
		}
	default:
		return nil
	}
}

func plistLabel(path string) string {
	out, err := exec.Command("plutil", "-extract", "Label", "raw", "-o", "-", path).Output()
	if err == nil {
		label := strings.TrimSpace(string(out))
		if label != "" {
			return label
		}
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func isLoadedService(label string, domains []string) bool {
	if label == "" {
		return false
	}
	for _, domain := range domains {
		target := fmt.Sprintf("%s/%s", domain, label)
		if err := exec.Command("launchctl", "print", target).Run(); err == nil {
			return true
		}
	}
	return false
}

func bootoutService(item CleanItem) {
	if !item.IsService {
		return
	}
	for _, domain := range item.ServiceDomains {
		_ = exec.Command("launchctl", "bootout", domain, item.Path).Run()
	}
}

func appendBootoutScript(b *strings.Builder, item CleanItem) {
	if !item.IsService {
		return
	}
	for _, domain := range item.ServiceDomains {
		fmt.Fprintf(b, "launchctl bootout '%s' '%s' 2>/dev/null || true\n",
			shellEscape(domain), shellEscape(item.Path))
	}
}

// ---------------------------------------------------------------------------
// Scanning
// ---------------------------------------------------------------------------

// Scan searches the system for cast-screen app remnants.
func Scan() []CleanItem {
	home := homeDir()
	var items []CleanItem

	// Audio HAL plugins
	scanDriverDir("/Library/Audio/Plug-Ins/HAL", CatAudioDriver, true,
		"Virtual audio HAL plugin", &items)

	// CoreMediaIO DAL plugins (virtual cameras)
	scanDriverDir("/Library/CoreMediaIO/Plug-Ins/DAL", CatVideoDriver, true,
		"Virtual camera plugin", &items)

	// System launch daemons
	scanServiceDir("/Library/LaunchDaemons", CatLaunchDaemon, true, &items)

	// System launch agents
	scanServiceDir("/Library/LaunchAgents", CatLaunchAgent, true, &items)

	// User launch agents
	scanServiceDir(filepath.Join(home, "Library", "LaunchAgents"), CatLaunchAgent, false, &items)

	// Application data (specific + generic)
	scanAppData(home, &items)

	// Preferences
	scanPrefs(home, &items)

	// Applications in /Applications
	scanApplications(&items)

	// Sort by category display order
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Category < items[j].Category
	})

	return items
}

func scanDriverDir(dir string, cat Category, sudo bool, desc string, items *[]CleanItem) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if containsKeyword(e.Name()) {
			path := filepath.Join(dir, e.Name())
			*items = append(*items, CleanItem{
				Category:    cat,
				Path:        path,
				Name:        e.Name(),
				Description: desc,
				NeedsSudo:   sudo,
				Size:        calcSize(path),
			})
		}
	}
}

func scanServiceDir(dir string, cat Category, sudo bool, items *[]CleanItem) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !containsKeyword(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		domains := serviceDomains(cat)
		label := plistLabel(path)
		isLoaded := isLoadedService(label, domains)

		desc := "Background service"
		if isLoaded {
			desc = "Background service (currently running)"
		}

		*items = append(*items, CleanItem{
			Category:       cat,
			Path:           path,
			Name:           e.Name(),
			Description:    desc,
			NeedsSudo:      sudo,
			IsService:      true,
			ServiceLabel:   label,
			ServiceDomains: domains,
			IsLoaded:       isLoaded,
			Size:           calcSize(path),
		})
	}
}

func scanApplications(items *[]CleanItem) {
	entries, err := os.ReadDir("/Applications")
	if err != nil {
		return
	}
	for _, e := range entries {
		if containsKeyword(e.Name()) {
			path := filepath.Join("/Applications", e.Name())
			// Check if we actually need sudo by testing write permission on the parent
			needsSudo := !isWritable(path)
			*items = append(*items, CleanItem{
				Category:    CatApplication,
				Path:        path,
				Name:        e.Name(),
				Description: "Application bundle",
				NeedsSudo:   needsSudo,
				Size:        calcSize(path),
			})
		}
	}
}

// isWritable checks whether the current user has write permission on the parent directory.
func isWritable(path string) bool {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".cast-screen-cleaner-probe-*")
	if err != nil {
		return false
	}
	name := tmp.Name()
	tmp.Close()
	os.Remove(name)
	return true
}

func scanAppData(home string, items *[]CleanItem) {
	seen := make(map[string]bool)

	// Known specific data directories
	known := []struct {
		path string
		desc string
	}{
		{filepath.Join(home, "Library", "UWST"), "UWST service and cached applications"},
		{filepath.Join(home, "Library", "TranscreenNew"), "Transscreen cached data"},
		{filepath.Join(home, "Library", "AnDisplay"), "AnDisplay launcher and cached data"},
	}
	for _, k := range known {
		if _, err := os.Stat(k.path); err == nil {
			seen[k.path] = true
			*items = append(*items, CleanItem{
				Category:    CatAppData,
				Path:        k.path,
				Name:        filepath.Base(k.path),
				Description: k.desc,
				Size:        calcSize(k.path),
			})
		}
	}

	// Generic scan of Application Support and Caches
	subdirs := []struct {
		name string
		desc string
	}{
		{"Application Support", "Application support data"},
		{"Caches", "Cached data"},
		{filepath.Join("Caches", "ScreenShare", "bundle"), "Screen recording cached app"},
	}
	for _, sub := range subdirs {
		dir := filepath.Join(home, "Library", sub.name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !containsKeyword(e.Name()) {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if seen[p] {
				continue
			}
			seen[p] = true
			*items = append(*items, CleanItem{
				Category:    CatAppData,
				Path:        p,
				Name:        e.Name(),
				Description: sub.desc,
				Size:        calcSize(p),
			})
		}
	}

	// System-level Application Support and Caches
	sysSubdirs := []struct {
		name string
		desc string
	}{
		{"Application Support", "System application support data"},
		{"Caches", "System cached data"},
	}
	for _, sub := range sysSubdirs {
		dir := filepath.Join("/Library", sub.name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !containsKeyword(e.Name()) {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if seen[p] {
				continue
			}
			seen[p] = true
			*items = append(*items, CleanItem{
				Category:    CatAppData,
				Path:        p,
				Name:        e.Name(),
				Description: sub.desc,
				NeedsSudo:   true,
				Size:        calcSize(p),
			})
		}
	}
}

func scanPrefs(home string, items *[]CleanItem) {
	dirs := []struct {
		path string
		sudo bool
	}{
		{filepath.Join(home, "Library", "Preferences"), false},
		{"/Library/Preferences", true},
	}
	for _, d := range dirs {
		entries, err := os.ReadDir(d.path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if containsKeyword(e.Name()) && strings.HasSuffix(e.Name(), ".plist") {
				path := filepath.Join(d.path, e.Name())
				*items = append(*items, CleanItem{
					Category:    CatPreference,
					Path:        path,
					Name:        e.Name(),
					Description: "Application preferences",
					NeedsSudo:   d.sudo,
					Size:        calcSize(path),
				})
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Removal
// ---------------------------------------------------------------------------

// shellEscape escapes a string for use inside single-quoted bash strings.
func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

// RemoveUserItems removes items that do NOT require sudo.
func RemoveUserItems(items []CleanItem) []RemoveResult {
	var results []RemoveResult
	for _, item := range items {
		if item.NeedsSudo {
			continue
		}
		bootoutService(item)
		err := os.RemoveAll(item.Path)
		r := RemoveResult{Item: item, Success: err == nil}
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
	}
	return results
}

// BuildSudoScript creates a bash script for removing items that need sudo.
func BuildSudoScript(items []CleanItem) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	// Unload services first
	for _, item := range items {
		appendBootoutScript(&b, item)
	}
	// Remove files
	for _, item := range items {
		fmt.Fprintf(&b, "rm -rf '%s'\n", shellEscape(item.Path))
	}
	return b.String()
}

// VerifySudoRemoval checks whether paths were actually removed after sudo.
func VerifySudoRemoval(items []CleanItem) []RemoveResult {
	var results []RemoveResult
	for _, item := range items {
		_, err := os.Stat(item.Path)
		removed := os.IsNotExist(err)
		r := RemoveResult{Item: item, Success: removed}
		if !removed {
			r.Error = "still exists (permission denied or in use)"
		}
		results = append(results, r)
	}
	return results
}

// HasRemovedAudioDriver returns true if any audio driver was successfully removed.
func HasRemovedAudioDriver(results []RemoveResult) bool {
	for _, r := range results {
		if r.Item.Category == CatAudioDriver && r.Success {
			return true
		}
	}
	return false
}
