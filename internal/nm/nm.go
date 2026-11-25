package nm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/ovpn"
)

// ProfileRecord maps a candidate server ID to a NetworkManager profile name.
type ProfileRecord struct {
	ProfileName string `json:"profile_name"`
	ServerID    string `json:"server_id"`
	IP          string `json:"ip"`
	CountryCode string `json:"country_code"`
}

// Manager manages `myovpn-*` OpenVPN profiles via `nmcli` (FR-10, FR-11, FR-12).
type Manager struct {
	RegistryPath string
	MaxProfiles  int
}

func NewManager(registryPath string, maxProfiles int) *Manager {
	if maxProfiles < 3 {
		maxProfiles = 3
	}
	if maxProfiles > 5 {
		maxProfiles = 5
	}
	return &Manager{
		RegistryPath: registryPath,
		MaxProfiles:  maxProfiles,
	}
}

// ProfileNameFor returns `myovpn-<CC>-<slot>` (1-based).
func ProfileNameFor(countryCode string, slot int) string {
	cc := strings.ToUpper(strings.TrimSpace(countryCode))
	if cc == "" {
		cc = "XX"
	}
	return fmt.Sprintf("%s%s-%d", config.ProfilePrefx, cc, slot)
}

// SyncCandidates ensures at most MaxProfiles `myovpn-*` profiles exist in NetworkManager,
// deleting any stale profiles first, then importing/updating the given candidates.
func (m *Manager) SyncCandidates(ctx context.Context, countryCode string, candidates []core.Server) ([]ProfileRecord, error) {
	if len(candidates) > m.MaxProfiles {
		candidates = candidates[:m.MaxProfiles]
	}

	desiredNames := make(map[string]bool, len(candidates))
	for i := range candidates {
		desiredNames[ProfileNameFor(countryCode, i+1)] = true
	}

	// FR-11: Delete any myovpn-* profile that is not part of this candidate set.
	existing, err := m.ListManagedProfiles(ctx)
	if err == nil {
		for _, name := range existing {
			if !desiredNames[name] {
				_ = m.DeleteProfile(ctx, name)
			}
		}
	}

	tmpDir, err := os.MkdirTemp("", "myovpn-nm-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	var records []ProfileRecord
	for i, srv := range candidates {
		profName := ProfileNameFor(countryCode, i+1)
		// Replace existing slot cleanly so stale server IP/config never lingers
		_ = m.DeleteProfile(ctx, profName)

		sanitized, err := ovpn.SanitizeAndValidate(srv.OVPNText, ovpn.SanitizeOptions{
			ExpectedIP:       srv.IP,
			ExpectedHostname: srv.Hostname,
		})
		if err != nil {
			continue
		}

		ovpnFile := filepath.Join(tmpDir, profName+".ovpn")
		if err := os.WriteFile(ovpnFile, []byte(sanitized.ConfigText), 0o600); err != nil {
			continue
		}

		if err := m.importProfile(ctx, profName, ovpnFile, srv); err != nil {
			continue
		}

		records = append(records, ProfileRecord{
			ProfileName: profName,
			ServerID:    srv.ID,
			IP:          srv.IP,
			CountryCode: strings.ToUpper(countryCode),
		})
	}

	_ = m.saveRegistry(records)
	return records, nil
}

func (m *Manager) importProfile(ctx context.Context, profName, ovpnFile string, srv core.Server) error {
	out, err := runNMCLI(ctx, "connection", "import", "type", "openvpn", "file", ovpnFile)
	if err != nil {
		return fmt.Errorf("nmcli import failed: %v (%s)", err, out)
	}

	// nmcli outputs: Connection 'myovpn-JP-1' (...) successfully added.
	importedID := parseImportedID(out, strings.TrimSuffix(filepath.Base(ovpnFile), ".ovpn"))
	if importedID != profName {
		_, _ = runNMCLI(ctx, "connection", "modify", importedID, "connection.id", profName)
	}
	_, _ = runNMCLI(ctx, "connection", "modify", profName, "connection.autoconnect", "no")

	if srv.Auth == core.AuthUserPass && srv.Username != "" {
		_, _ = runNMCLI(ctx, "connection", "modify", profName,
			"+vpn.data", "username="+srv.Username,
			"vpn.secrets", "password="+srv.Password,
		)
	}
	return nil
}

// Up activates the named NetworkManager profile.
func (m *Manager) Up(ctx context.Context, profileName string, timeout time.Duration) error {
	if !strings.HasPrefix(profileName, config.ProfilePrefx) {
		return fmt.Errorf("refusing to touch non-myovpn profile %q", profileName)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := runNMCLI(ctx, "connection", "up", "id", profileName)
	if err != nil {
		return fmt.Errorf("nmcli up %s: %v (%s)", profileName, err, out)
	}
	return nil
}

// Down deactivates the named NetworkManager profile.
func (m *Manager) Down(ctx context.Context, profileName string) error {
	if !strings.HasPrefix(profileName, config.ProfilePrefx) {
		return fmt.Errorf("refusing to touch non-myovpn profile %q", profileName)
	}
	out, err := runNMCLI(ctx, "connection", "down", "id", profileName)
	if err != nil {
		return fmt.Errorf("nmcli down %s: %v (%s)", profileName, err, out)
	}
	return nil
}

// DeleteProfile deletes a single `myovpn-*` profile (never touches non-myovpn profiles).
func (m *Manager) DeleteProfile(ctx context.Context, profileName string) error {
	if !strings.HasPrefix(profileName, config.ProfilePrefx) {
		return fmt.Errorf("refusing to delete non-myovpn profile %q", profileName)
	}
	_, err := runNMCLI(ctx, "connection", "delete", "id", profileName)
	return err
}

// ListManagedProfiles returns all NetworkManager connection IDs with the `myovpn-` prefix.
func (m *Manager) ListManagedProfiles(ctx context.Context) ([]string, error) {
	out, err := runNMCLI(ctx, "-t", "-f", "NAME", "connection", "show")
	if err != nil {
		return nil, err
	}
	var managed []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if strings.HasPrefix(name, config.ProfilePrefx) {
			managed = append(managed, name)
		}
	}
	return managed, nil
}

// CleanupAll deletes every `myovpn-*` profile in NetworkManager and clears the registry.
func (m *Manager) CleanupAll(ctx context.Context) error {
	profiles, err := m.ListManagedProfiles(ctx)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		_ = m.Down(ctx, p)
		_ = m.DeleteProfile(ctx, p)
	}
	_ = os.Remove(m.RegistryPath)
	return nil
}

func (m *Manager) saveRegistry(records []ProfileRecord) error {
	if m.RegistryPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.RegistryPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.RegistryPath, b, 0o600)
}

func parseImportedID(out, fallback string) string {
	// Look for single-quoted connection name: Connection 'name' (...) successfully added.
	start := strings.IndexByte(out, '\'')
	if start >= 0 {
		rest := out[start+1:]
		if end := strings.IndexByte(rest, '\''); end > 0 {
			return rest[:end]
		}
	}
	return fallback
}

func runNMCLI(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "nmcli", args...)
	b, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(b)), err
}
