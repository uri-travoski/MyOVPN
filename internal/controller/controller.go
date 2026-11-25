package controller

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/health"
	"github.com/uri-travoski/MyOVPN/internal/netns"
	"github.com/uri-travoski/MyOVPN/internal/nm"
	"github.com/uri-travoski/MyOVPN/internal/sources"
	"github.com/uri-travoski/MyOVPN/internal/winvpn"
)

// EventSink receives asynchronous notifications from the controller.
type EventSink interface {
	OnNotify(title, body string)
	OnStatus(snap core.StatusSnapshot)
}

// Controller implements the server list, ranking, connection, health-check, and failover loop.
type Controller struct {
	mu          sync.Mutex
	cfg         *config.Config
	cfgPath     string
	cachePath   string
	registry    string
	blacklist   *core.Blacklist
	servers     []core.Server
	sourceUsed  string
	usingCache  bool
	cacheAge    time.Duration
	state       core.ConnectionState
	countryCode string
	countryName string
	candidates  []core.Server
	activeIdx   int
	lastError   string
	noticeSeq   uint64
	lastNotice  *core.Notice

	nmMgr     *nm.Manager
	netnsRun  *netns.Runner
	winRun    *winvpn.Runner
	sink      EventSink
	logf      func(string, ...any)
	opCancel  context.CancelFunc
	checkNow  chan struct{}
	retryStop chan struct{}
	closed    bool
}

// New creates a Controller configured for the given user paths.
func New(cfg *config.Config, cfgPath, cachePath, nmRegistryPath string, sink EventSink, logf func(string, ...any)) *Controller {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	onLog := func(msg string) { logf("%s", msg) }
	c := &Controller{
		cfg:       cfg,
		cfgPath:   cfgPath,
		cachePath: cachePath,
		registry:  nmRegistryPath,
		blacklist: core.NewBlacklist(time.Duration(cfg.BlacklistMinutes) * time.Minute),
		state:     core.StateDisconnected,
		nmMgr:     nm.NewManager(nmRegistryPath, cfg.MaxProfiles),
		netnsRun:  netns.NewRunner(onLog),
		winRun:    winvpn.NewRunner(onLog),
		sink:      sink,
		logf:      logf,
		checkNow:  make(chan struct{}, 1),
	}
	return c
}

// StartupCleanup removes any stale profiles or leftover namespace from a prior crash (FR-11, FR-13).
func (c *Controller) StartupCleanup(ctx context.Context) {
	if runtime.GOOS == "linux" {
		if c.cfg.Mode == "system-wide" || config.IsFlatpak() {
			_ = c.nmMgr.CleanupAll(ctx)
		} else if os.Geteuid() == 0 {
			_ = netns.CleanupAll(ctx)
			_ = c.nmMgr.CleanupAll(ctx)
		}
	}
}

// Status returns a thread-safe snapshot of current state and countries.
func (c *Controller) Status() core.StatusSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

func (c *Controller) snapshotLocked() core.StatusSnapshot {
	opts := core.FilterOptions{
		MinSpeedMbps:     c.cfg.MinSpeedMbps,
		MinUptimeMinutes: c.cfg.MinUptimeMinutes,
	}
	countries := core.SummarizeCountries(c.servers, c.cfg.RecentCountries, opts, c.blacklist, time.Now())

	var active *core.Server
	if c.activeIdx >= 0 && c.activeIdx < len(c.candidates) && c.state != core.StateDisconnected {
		srv := c.candidates[c.activeIdx]
		srv.OVPNText = "" // omit large ovpn payload from status RPC
		active = &srv
	}
	candsCopy := make([]core.Server, len(c.candidates))
	for i, s := range c.candidates {
		s.OVPNText = ""
		candsCopy[i] = s
	}

	var ageStr string
	if c.usingCache && c.cacheAge > 0 {
		ageStr = c.cacheAge.Round(time.Minute).String()
	}

	return core.StatusSnapshot{
		State:         c.state,
		Mode:          c.cfg.Mode,
		CountryCode:   c.countryCode,
		CountryName:   c.countryName,
		ActiveServer:  active,
		Candidates:    candsCopy,
		LastError:     c.lastError,
		SourceUsed:    c.sourceUsed,
		UsingCache:    c.usingCache,
		CacheAge:      ageStr,
		Countries:     countries,
		Notice:        c.lastNotice,
		UpdatedAt:     time.Now(),
		PerAppReady:   runtime.GOOS == "linux" && !config.IsFlatpak() && os.Geteuid() == 0,
		HelperRunning: true,
	}
}

func (c *Controller) notify(title, body string) {
	c.mu.Lock()
	c.noticeSeq++
	n := &core.Notice{Seq: c.noticeSeq, Title: title, Body: body}
	c.lastNotice = n
	notifsEnabled := c.cfg.Notifications
	c.mu.Unlock()

	if c.sink != nil && notifsEnabled {
		c.sink.OnNotify(title, body)
	}
}

func (c *Controller) emitStatus() {
	if c.sink != nil {
		c.sink.OnStatus(c.Status())
	}
}

// Refresh reloads server lists from enabled sources (or cache on total failure).
func (c *Controller) Refresh(ctx context.Context, force bool) core.StatusSnapshot {
	c.logf("refreshing server list (force=%v)", force)
	res := sources.FetchAll(
		ctx,
		&http.Client{Timeout: 25 * time.Second},
		c.cfg.SourceOrder,
		c.cfg.EnabledSources,
		c.cfg.CustomURLs,
		c.cfg.ImportDir,
		c.cachePath,
		force,
	)

	c.mu.Lock()
	if len(res.Servers) > 0 {
		c.servers = res.Servers
		c.sourceUsed = res.SourceUsed
		c.usingCache = res.UsingCache
		c.cacheAge = res.CacheAge
		if c.state == core.StateError && c.countryCode == "" {
			c.state = core.StateDisconnected
			c.lastError = ""
		}
	} else if len(c.servers) == 0 {
		c.state = core.StateError
		c.lastError = "All server sources unreachable"
	}
	snap := c.snapshotLocked()
	c.mu.Unlock()

	c.emitStatus()
	return snap
}

// ReloadConfig reloads config.toml from disk without interrupting active tunnels.
func (c *Controller) ReloadConfig(cfg *config.Config) {
	c.mu.Lock()
	c.cfg = cfg
	c.nmMgr.MaxProfiles = cfg.MaxProfiles
	c.mu.Unlock()
	c.emitStatus()
}

// SetMode changes routing mode (`per-app` or `system-wide`) and disconnects any active tunnel first.
func (c *Controller) SetMode(ctx context.Context, mode string) error {
	if mode != "per-app" && mode != "system-wide" {
		return fmt.Errorf("invalid mode %q", mode)
	}
	if (runtime.GOOS == "windows" || config.IsFlatpak()) && mode == "per-app" {
		return fmt.Errorf("per-app mode is not supported on this platform")
	}
	_ = c.Disconnect(ctx)
	c.mu.Lock()
	c.cfg.Mode = mode
	if c.cfgPath != "" {
		_ = config.Save(c.cfgPath, c.cfg)
	}
	c.mu.Unlock()
	c.emitStatus()
	return nil
}

// Connect starts the connection and failover loop for the given country (and optional server ID).
func (c *Controller) Connect(ctx context.Context, countryCode, serverID string) error {
	cc := strings.ToUpper(strings.TrimSpace(countryCode))
	if cc == "" {
		return fmt.Errorf("country code is required")
	}

	c.cancelActiveLocked()

	c.mu.Lock()
	if len(c.servers) == 0 {
		c.mu.Unlock()
		c.Refresh(ctx, false)
		c.mu.Lock()
	}

	c.cfg.RecordRecentCountry(cc)
	if c.cfgPath != "" {
		_ = config.Save(c.cfgPath, c.cfg)
	}
	c.countryCode = cc
	c.countryName = c.findCountryNameLocked(cc)
	c.state = core.StateConnecting
	c.lastError = ""
	c.activeIdx = 0

	opCtx, cancel := context.WithCancel(context.Background())
	c.opCancel = cancel
	c.mu.Unlock()

	c.emitStatus()
	go c.runFailoverLoop(opCtx, cc, serverID)
	return nil
}

// NextServer switches immediately to the next candidate server for the active country.
func (c *Controller) NextServer(ctx context.Context) error {
	c.mu.Lock()
	if c.countryCode == "" || len(c.candidates) == 0 {
		c.mu.Unlock()
		return fmt.Errorf("no active country selected")
	}
	nextIdx := (c.activeIdx + 1) % len(c.candidates)
	targetID := c.candidates[nextIdx].ID
	cc := c.countryCode
	c.mu.Unlock()

	return c.Connect(ctx, cc, targetID)
}

// Disconnect stops the active tunnel without destroying per-app apps or NM candidate profiles.
func (c *Controller) Disconnect(ctx context.Context) error {
	c.cancelActiveLocked()

	c.mu.Lock()
	mode := c.cfg.Mode
	cc := c.countryCode
	idx := c.activeIdx
	c.state = core.StateDisconnected
	c.lastError = ""
	c.mu.Unlock()

	c.stopTunnelOnly(ctx, mode, cc, idx)
	c.emitStatus()
	return nil
}

// Quit disconnects and cleans up all namespaces, nftables rules, and `myovpn-*` profiles (FR-11, FR-13).
func (c *Controller) Quit(ctx context.Context) error {
	c.cancelActiveLocked()

	c.mu.Lock()
	c.closed = true
	c.state = core.StateDisconnected
	c.mu.Unlock()

	c.winRun.StopOpenVPN()
	if runtime.GOOS == "linux" {
		_ = c.netnsRun.Cleanup(ctx)
		_ = c.nmMgr.CleanupAll(ctx)
	}
	return nil
}

// TriggerHealthCheck wakes the active health monitor immediately (e.g., on resume from suspend).
func (c *Controller) TriggerHealthCheck() {
	select {
	case c.checkNow <- struct{}{}:
	default:
	}
}

func (c *Controller) cancelActiveLocked() {
	c.mu.Lock()
	cancel := c.opCancel
	c.opCancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Controller) findCountryNameLocked(cc string) string {
	for _, s := range c.servers {
		if strings.EqualFold(s.CountryCode, cc) && s.CountryName != "" {
			return s.CountryName
		}
	}
	return cc
}

func (c *Controller) runFailoverLoop(ctx context.Context, cc, preferredServerID string) {
	refetchAttempts := 0

	for {
		if ctx.Err() != nil {
			return
		}

		candidates := c.prepareCandidates(ctx, cc, preferredServerID)
		preferredServerID = "" // only apply preference on initial round

		if len(candidates) == 0 {
			refetchAttempts++
			if refetchAttempts >= 3 {
				c.enterRetryWait(ctx, cc, fmt.Sprintf("No working servers in %s right now", cc))
				refetchAttempts = 0
				continue
			}
			c.Refresh(ctx, true)
			continue
		}

		allCandidatesFailed := true
		for i := 0; i < len(candidates); i++ {
			if ctx.Err() != nil {
				return
			}
			srv := candidates[i]

			c.mu.Lock()
			c.state = core.StateConnecting
			c.activeIdx = i
			c.lastError = ""
			mode := c.cfg.Mode
			c.mu.Unlock()
			c.emitStatus()

			if i > 0 {
				c.notify("MyOVPN", fmt.Sprintf("Switched to server %d of %d (%s · %s)", i+1, len(candidates), srv.IP, srv.CountryName))
			}

			if err := c.connectAndVerify(ctx, mode, cc, i, srv); err != nil {
				if ctx.Err() != nil {
					return
				}
				c.logf("candidate %s (%s) failed: %v", srv.ID, srv.IP, err)
				c.blacklist.Add(srv.IP, time.Now())
				c.stopTunnelOnly(context.Background(), mode, cc, i)
				if mode == "system-wide" && runtime.GOOS == "linux" {
					_ = c.nmMgr.DeleteProfile(context.Background(), nm.ProfileNameFor(cc, i+1))
				}
				continue
			}

			// Connected & initial health check passed!
			refetchAttempts = 0
			allCandidatesFailed = false
			c.mu.Lock()
			c.state = core.StateConnected
			c.lastError = ""
			c.mu.Unlock()
			c.emitStatus()
			c.notify("MyOVPN Connected", fmt.Sprintf("%s · %s · %s", srv.CountryName, srv.IP, srv.FormatSpeed()))

			// Monitor health until 2 consecutive failures or cancellation
			reason := c.monitorHealth(ctx, mode)
			if ctx.Err() != nil {
				return
			}
			c.logf("server %s lost health: %s", srv.IP, reason)
			c.blacklist.Add(srv.IP, time.Now())
			c.stopTunnelOnly(context.Background(), mode, cc, i)
			if mode == "system-wide" && runtime.GOOS == "linux" {
				_ = c.nmMgr.DeleteProfile(context.Background(), nm.ProfileNameFor(cc, i+1))
			}
			c.notify("MyOVPN Failover", fmt.Sprintf("Switching from %s (%s)", srv.IP, reason))
		}

		if allCandidatesFailed || ctx.Err() == nil {
			refetchAttempts++
			if refetchAttempts >= 3 {
				c.enterRetryWait(ctx, cc, fmt.Sprintf("No working servers in %s right now", cc))
				refetchAttempts = 0
				continue
			}
			c.logf("all candidates exhausted for %s; refetching list (attempt %d/3)", cc, refetchAttempts)
			c.Refresh(ctx, true)
		}
	}
}

func (c *Controller) prepareCandidates(ctx context.Context, cc, preferredID string) []core.Server {
	c.mu.Lock()
	opts := core.FilterOptions{
		MinSpeedMbps:     c.cfg.MinSpeedMbps,
		MinUptimeMinutes: c.cfg.MinUptimeMinutes,
	}
	maxCand := c.cfg.MaxProfiles
	if maxCand < 3 {
		maxCand = 3
	}
	cands := core.TopCandidates(c.servers, cc, maxCand, opts, c.blacklist, time.Now())

	if preferredID != "" {
		found := -1
		for i, s := range cands {
			if s.ID == preferredID {
				found = i
				break
			}
		}
		if found > 0 {
			cands[0], cands[found] = cands[found], cands[0]
		} else if found < 0 {
			for _, s := range c.servers {
				if s.ID == preferredID && strings.EqualFold(s.CountryCode, cc) {
					cands = append([]core.Server{s}, cands...)
					if len(cands) > maxCand {
						cands = cands[:maxCand]
					}
					break
				}
			}
		}
	}

	c.candidates = cands
	mode := c.cfg.Mode
	c.mu.Unlock()

	if mode == "system-wide" && runtime.GOOS == "linux" && len(cands) > 0 {
		_, _ = c.nmMgr.SyncCandidates(ctx, cc, cands)
	}
	return cands
}

func (c *Controller) connectAndVerify(ctx context.Context, mode, cc string, slotIdx int, srv core.Server) error {
	timeout := time.Duration(c.cfg.ConnectTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	switch {
	case runtime.GOOS == "windows":
		if err := c.winRun.StartOpenVPN(ctx, srv, timeout); err != nil {
			return err
		}
	case mode == "per-app":
		if err := c.netnsRun.StartOpenVPN(ctx, srv, timeout); err != nil {
			return err
		}
	default:
		prof := nm.ProfileNameFor(cc, slotIdx+1)
		if err := c.nmMgr.Up(ctx, prof, timeout); err != nil {
			return err
		}
	}

	netnsName := ""
	if runtime.GOOS == "linux" && mode == "per-app" {
		netnsName = config.NetnsName
	}

	checkTimeout := time.Duration(c.cfg.HealthCheckTimeoutSec) * time.Second
	if err := health.Check(ctx, c.cfg.HealthCheckURL, checkTimeout, netnsName); err != nil {
		return fmt.Errorf("initial health check: %w", err)
	}
	if c.cfg.GeoIPCheckEnabled {
		if err := health.CheckGeoIP(ctx, c.cfg.GeoIPURL, cc, checkTimeout, netnsName); err != nil {
			return fmt.Errorf("geoip check: %w", err)
		}
	}
	return nil
}

func (c *Controller) monitorHealth(ctx context.Context, mode string) string {
	interval := time.Duration(c.cfg.HealthCheckIntervalSec) * time.Second
	if interval <= 0 {
		interval = 20 * time.Second
	}
	timeout := time.Duration(c.cfg.HealthCheckTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	netnsName := ""
	if runtime.GOOS == "linux" && mode == "per-app" {
		netnsName = config.NetnsName
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	consecutiveFailures := 0

	for {
		select {
		case <-ctx.Done():
			return "cancelled"
		case <-ticker.C:
		case <-c.checkNow:
			c.logf("running immediate health check after resume/network event")
		}

		err := health.Check(ctx, c.cfg.HealthCheckURL, timeout, netnsName)
		if err != nil {
			consecutiveFailures++
			c.logf("health check failure %d/2: %v", consecutiveFailures, err)
			if consecutiveFailures >= 2 {
				return "no traffic"
			}
		} else {
			consecutiveFailures = 0
		}
	}
}

func (c *Controller) enterRetryWait(ctx context.Context, cc, msg string) {
	c.mu.Lock()
	c.state = core.StateError
	c.lastError = msg
	c.mu.Unlock()
	c.emitStatus()
	c.notify("MyOVPN Error", msg)

	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
		c.Refresh(ctx, true)
	}
}

func (c *Controller) stopTunnelOnly(ctx context.Context, mode, cc string, slotIdx int) {
	if runtime.GOOS == "windows" {
		c.winRun.StopOpenVPN()
		return
	}
	if mode == "per-app" {
		c.netnsRun.StopOpenVPN()
		return
	}
	if cc != "" && slotIdx >= 0 {
		_ = c.nmMgr.Down(ctx, nm.ProfileNameFor(cc, slotIdx+1))
	}
}

// LaunchApp launches a configured user application inside the per-app namespace.
func (c *Controller) LaunchApp(uid, gid uint32, env []string, appID string) error {
	c.mu.Lock()
	mode := c.cfg.Mode
	var target *config.AppEntry
	for i := range c.cfg.Apps {
		if c.cfg.Apps[i].ID == appID {
			a := c.cfg.Apps[i]
			target = &a
			break
		}
	}
	c.mu.Unlock()

	if target == nil {
		return fmt.Errorf("unknown app id %q", appID)
	}
	if runtime.GOOS != "linux" || mode != "per-app" {
		return fmt.Errorf("per-app launching requires Linux per-app mode")
	}
	return netns.LaunchAppAsUser(uid, gid, env, *target)
}

// DefaultRegistryPath returns the NetworkManager profile registry file path.
func DefaultRegistryPath() string {
	if os.Geteuid() == 0 {
		return "/var/lib/myovpn/profiles.json"
	}
	return filepath.Join(config.StateDir(), "profiles.json")
}
