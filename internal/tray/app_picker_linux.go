//go:build linux

package tray

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/uri-travoski/MyOVPN/internal/apps"
	"github.com/uri-travoski/MyOVPN/internal/config"
)

//go:embed app_picker.py
var appPickerScript string

type pickerInput struct {
	Shortcuts   []apps.DesktopShortcut `json:"shortcuts"`
	CurrentApps []config.AppEntry      `json:"current_apps"`
}

type pickerOutput struct {
	Saved bool `json:"saved"`
	Apps  []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		CommandLine string `json:"command_line"`
	} `json:"apps"`
}

// RunManageAppsDialog opens the native GTK3/Tkinter GUI app picker and returns the updated Apps list.
func RunManageAppsDialog(current []config.AppEntry) ([]config.AppEntry, bool, error) {
	in := pickerInput{
		Shortcuts:   apps.ScanDesktopShortcuts(),
		CurrentApps: current,
	}
	inBytes, err := json.Marshal(in)
	if err != nil {
		return nil, false, err
	}

	if _, err := exec.LookPath("python3"); err == nil {
		cmd := exec.Command("python3", "-c", appPickerScript)
		cmd.Stdin = bytes.NewReader(inBytes)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, false, fmt.Errorf("app picker failed: %v (%s)", err, strings.TrimSpace(stderr.String()))
		}
		var out pickerOutput
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			return nil, false, err
		}
		if !out.Saved {
			return nil, false, nil
		}

		// Rebuild AppEntry list preserving existing IDs where unchanged
		existingByID := make(map[string]config.AppEntry, len(current))
		for _, a := range current {
			existingByID[a.ID] = a
		}
		var updated []config.AppEntry
		for _, item := range out.Apps {
			cmdPath, args, err := apps.ParseCommandLine(item.CommandLine)
			if err != nil {
				continue
			}
			if item.ID != "" {
				if prev, ok := existingByID[item.ID]; ok {
					prev.Name = item.Name
					prev.Command = cmdPath
					prev.Args = args
					updated = append(updated, prev)
					continue
				}
			}
			entry, err := apps.BuildAppEntry(item.Name, item.CommandLine, updated)
			if err == nil {
				updated = append(updated, entry)
			}
		}
		return updated, true, nil
	}

	// Zenity fallback when python3 is absent
	if _, err := exec.LookPath("zenity"); err == nil {
		chosenBytes, err := exec.Command("zenity", "--file-selection", "--title=Select App Executable or .desktop Shortcut").Output()
		if err != nil {
			return nil, false, nil
		}
		chosen := strings.TrimSpace(string(chosenBytes))
		if chosen == "" {
			return nil, false, nil
		}
		name := ""
		cmdLine := chosen
		if strings.HasSuffix(strings.ToLower(chosen), ".desktop") {
			if sc, err := apps.ParseDesktopFile(chosen); err == nil {
				name = sc.Name
				cmdLine = sc.CommandLine
			}
		}
		entry, err := apps.BuildAppEntry(name, cmdLine, current)
		if err != nil {
			return nil, false, err
		}
		return append(append([]config.AppEntry{}, current...), entry), true, nil
	}

	return nil, false, fmt.Errorf("no GUI toolkit (python3-gi, tkinter, or zenity) found")
}
