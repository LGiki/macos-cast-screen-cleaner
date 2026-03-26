package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlistLabelUsesLabelKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unexpected-name.plist")
	content := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.example.real-label</string>
</dict>
</plist>
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	if got := plistLabel(path); got != "com.example.real-label" {
		t.Fatalf("plistLabel() = %q, want %q", got, "com.example.real-label")
	}
}

func TestServiceDomainsMatchLaunchdTargets(t *testing.T) {
	agentDomains := serviceDomains(CatLaunchAgent)
	wantAgent := []string{
		fmt.Sprintf("gui/%d", os.Getuid()),
		fmt.Sprintf("user/%d", os.Getuid()),
	}
	if len(agentDomains) != len(wantAgent) {
		t.Fatalf("serviceDomains(agent) len = %d, want %d", len(agentDomains), len(wantAgent))
	}
	for i, want := range wantAgent {
		if agentDomains[i] != want {
			t.Fatalf("serviceDomains(agent)[%d] = %q, want %q", i, agentDomains[i], want)
		}
	}

	daemonDomains := serviceDomains(CatLaunchDaemon)
	if len(daemonDomains) != 1 || daemonDomains[0] != "system" {
		t.Fatalf("serviceDomains(daemon) = %v, want [system]", daemonDomains)
	}
}

func TestBuildSudoScriptUsesBootoutForEachServiceDomain(t *testing.T) {
	agentPath := "/Library/LaunchAgents/com.example's-agent.plist"
	daemonPath := "/Library/LaunchDaemons/com.example.daemon.plist"
	items := []CleanItem{
		{
			Path:           daemonPath,
			IsService:      true,
			ServiceDomains: []string{"system"},
		},
		{
			Path:           agentPath,
			IsService:      true,
			ServiceDomains: []string{"gui/501", "user/501"},
		},
		{
			Path: "/Library/Application Support/CVTE",
		},
	}

	script := BuildSudoScript(items)

	wantLines := []string{
		fmt.Sprintf("launchctl bootout 'system' '%s' 2>/dev/null || true", shellEscape(daemonPath)),
		fmt.Sprintf("launchctl bootout 'gui/501' '%s' 2>/dev/null || true", shellEscape(agentPath)),
		fmt.Sprintf("launchctl bootout 'user/501' '%s' 2>/dev/null || true", shellEscape(agentPath)),
		fmt.Sprintf("rm -rf '%s'", shellEscape(daemonPath)),
		fmt.Sprintf("rm -rf '%s'", shellEscape(agentPath)),
		"rm -rf '/Library/Application Support/CVTE'",
	}
	for _, want := range wantLines {
		if !strings.Contains(script, want) {
			t.Fatalf("BuildSudoScript() missing line %q\nscript:\n%s", want, script)
		}
	}

	if strings.Contains(script, "launchctl unload") {
		t.Fatalf("BuildSudoScript() still uses legacy unload:\n%s", script)
	}
}
