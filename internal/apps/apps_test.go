package apps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uri-travoski/MyOVPN/internal/config"
)

func TestParseCommandLine(t *testing.T) {
	cmd, args, err := ParseCommandLine(`/usr/lib/chromium/chromium --user-data-dir=/home/owner/Applications/Chromium/profile-1 %U`)
	if err != nil {
		t.Fatalf("ParseCommandLine error: %v", err)
	}
	if cmd != "/usr/lib/chromium/chromium" {
		t.Fatalf("unexpected cmd: %q", cmd)
	}
	if len(args) != 1 || args[0] != "--user-data-dir=/home/owner/Applications/Chromium/profile-1" {
		t.Fatalf("unexpected args: %v", args)
	}

	cmd2, args2, err := ParseCommandLine(`"/home/owner/My Apps/antigravity" --flag "hello world" %F`)
	if err != nil {
		t.Fatalf("ParseCommandLine quoted error: %v", err)
	}
	if cmd2 != "/home/owner/My Apps/antigravity" || len(args2) != 2 || args2[1] != "hello world" {
		t.Fatalf("unexpected quoted parse: cmd=%q args=%v", cmd2, args2)
	}
}

func TestParseDesktopFileAndBuildAppEntry(t *testing.T) {
	tmp := t.TempDir()
	desktop := filepath.Join(tmp, "chromium-p1.desktop")
	content := `[Desktop Entry]
Type=Application
Name=Chromium Profile 1
Exec=/usr/lib/chromium/chromium --user-data-dir=/home/owner/Applications/Chromium/profile-1 %U
Comment=Browse web
`
	if err := os.WriteFile(desktop, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := ParseDesktopFile(desktop)
	if err != nil {
		t.Fatalf("ParseDesktopFile error: %v", err)
	}
	if sc.Name != "Chromium Profile 1" || sc.CommandLine != "/usr/lib/chromium/chromium --user-data-dir=/home/owner/Applications/Chromium/profile-1" {
		t.Fatalf("unexpected shortcut: %+v", sc)
	}

	existing := []config.AppEntry{{ID: "chromium-profile-1"}}
	entry, err := BuildAppEntry(sc.Name, sc.CommandLine, existing)
	if err != nil {
		t.Fatalf("BuildAppEntry error: %v", err)
	}
	if entry.ID != "chromium-profile-1-2" || entry.Command != "/usr/lib/chromium/chromium" {
		t.Fatalf("unexpected built entry: %+v", entry)
	}
}
