package apps

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/uri-travoski/MyOVPN/internal/config"
)

// DesktopShortcut represents an application entry parsed from a `.desktop` file.
type DesktopShortcut struct {
	Name        string `json:"name"`
	CommandLine string `json:"command_line"`
	DesktopFile string `json:"desktop_file"`
	Comment     string `json:"comment,omitempty"`
}

// ParseCommandLine tokenizes a command string (supporting quotes, backslash escapes,
// and stripping freedesktop Exec placeholders like %u, %U, %f, %F) into command + args.
func ParseCommandLine(raw string) (string, []string, error) {
	tokens := splitShellArgs(raw)
	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}
	var cleaned []string
	for _, tok := range tokens {
		if isFreedesktopFieldCode(tok) {
			continue
		}
		cleaned = append(cleaned, tok)
	}
	if len(cleaned) == 0 {
		return "", nil, fmt.Errorf("empty command after removing placeholders")
	}
	return cleaned[0], cleaned[1:], nil
}

func isFreedesktopFieldCode(tok string) bool {
	switch tok {
	case "%f", "%F", "%u", "%U", "%d", "%D", "%n", "%N", "%i", "%c", "%k", "%v", "%m":
		return true
	default:
		return false
	}
}

func splitShellArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inToken := false
	inSingle := false
	inDouble := false
	escaped := false

	for _, r := range s {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			inToken = true
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			inToken = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			inToken = true
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			inToken = true
			continue
		}
		if unicode.IsSpace(r) && !inSingle && !inDouble {
			if inToken {
				args = append(args, cur.String())
				cur.Reset()
				inToken = false
			}
			continue
		}
		cur.WriteRune(r)
		inToken = true
	}
	if inToken {
		args = append(args, cur.String())
	}
	return args
}

// ParseDesktopFile parses a `.desktop` file into a DesktopShortcut.
func ParseDesktopFile(path string) (DesktopShortcut, error) {
	f, err := os.Open(path)
	if err != nil {
		return DesktopShortcut{}, err
	}
	defer f.Close()

	var sc DesktopShortcut
	sc.DesktopFile = path
	inDesktopEntry := false
	noDisplay := false
	hidden := false
	appType := ""

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDesktopEntry = (line == "[Desktop Entry]")
			continue
		}
		if !inDesktopEntry {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		switch k {
		case "Type":
			appType = v
		case "NoDisplay":
			noDisplay = strings.EqualFold(v, "true")
		case "Hidden":
			hidden = strings.EqualFold(v, "true")
		case "Name":
			if sc.Name == "" {
				sc.Name = v
			}
		case "Exec":
			if sc.CommandLine == "" {
				sc.CommandLine = cleanExecDisplay(v)
			}
		case "Comment":
			if sc.Comment == "" {
				sc.Comment = v
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return DesktopShortcut{}, err
	}
	if appType != "" && !strings.EqualFold(appType, "Application") {
		return DesktopShortcut{}, fmt.Errorf("not an Application entry")
	}
	if noDisplay || hidden {
		return DesktopShortcut{}, fmt.Errorf("hidden desktop entry")
	}
	if sc.Name == "" || sc.CommandLine == "" {
		return DesktopShortcut{}, fmt.Errorf("missing Name or Exec")
	}
	return sc, nil
}

func cleanExecDisplay(execStr string) string {
	toks := splitShellArgs(execStr)
	var out []string
	for _, t := range toks {
		if isFreedesktopFieldCode(t) {
			continue
		}
		if strings.ContainsAny(t, " \t") {
			out = append(out, `"`+strings.ReplaceAll(t, `"`, `\"`)+`"`)
		} else {
			out = append(out, t)
		}
	}
	return strings.Join(out, " ")
}

// ScanDesktopShortcuts returns sorted `.desktop` Application shortcuts from standard user & system dirs.
func ScanDesktopShortcuts() []DesktopShortcut {
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(home, ".local", "share", "applications"),
		filepath.Join(home, "Desktop"),
		"/usr/local/share/applications",
		"/usr/share/applications",
	}

	seen := make(map[string]bool)
	var out []DesktopShortcut
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".desktop") {
				continue
			}
			if strings.HasPrefix(e.Name(), "myovpn") {
				continue
			}
			sc, err := ParseDesktopFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			key := strings.ToLower(sc.Name + "|" + sc.CommandLine)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, sc)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// BuildAppEntry constructs a config.AppEntry with a unique slug ID.
func BuildAppEntry(name, commandLine string, existing []config.AppEntry) (config.AppEntry, error) {
	cmd, args, err := ParseCommandLine(commandLine)
	if err != nil {
		return config.AppEntry{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(cmd)
	}
	baseID := slugify(name)
	if baseID == "" {
		baseID = "app"
	}
	used := make(map[string]bool, len(existing))
	for _, e := range existing {
		used[e.ID] = true
	}
	id := baseID
	for n := 2; used[id]; n++ {
		id = fmt.Sprintf("%s-%d", baseID, n)
	}
	return config.AppEntry{
		ID:      id,
		Name:    name,
		Command: cmd,
		Args:    args,
	}, nil
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
