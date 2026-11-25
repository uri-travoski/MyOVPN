package tray

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/uri-travoski/MyOVPN/internal/assets"
	"github.com/uri-travoski/MyOVPN/internal/autostart"
	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/notify"
	"github.com/uri-travoski/MyOVPN/internal/winservice"
)

const countriesPerPage = 10

// App runs the cross-platform MyOVPN system tray UI.
type App struct {
	cfgPath string
	cfg     *config.Config
	backend Backend
	logf    func(string, ...any)

	mu            sync.Mutex
	snap          core.StatusSnapshot
	page          int
	pageCountries []core.CountrySummary
	candidates    []core.Server
	autoLaunched  bool
	prevState     core.ConnectionState
	lastNoticeSeq uint64
	stopCh        chan struct{}

	// Menu items
	mStatus         *systray.MenuItem
	mChooseCountry  *systray.MenuItem
	mCountrySlots   [countriesPerPage]*systray.MenuItem
	mPrevPage       *systray.MenuItem
	mPageLabel      *systray.MenuItem
	mNextPage       *systray.MenuItem
	mCacheNotice    *systray.MenuItem
	mRefresh        *systray.MenuItem
	mServerMenu     *systray.MenuItem
	mServerSlots    [5]*systray.MenuItem
	mNextServer     *systray.MenuItem
	mLaunchChrome   *systray.MenuItem
	mDisconnect     *systray.MenuItem
	mSettings       *systray.MenuItem
	mModePerApp     *systray.MenuItem
	mModeSystemWide *systray.MenuItem
	mStartAtLogin   *systray.MenuItem
	mNotifications  *systray.MenuItem
	mAutoLaunch     *systray.MenuItem
	mEnablePerApp   *systray.MenuItem
	mRemoveHelper   *systray.MenuItem
	mEditSettings   *systray.MenuItem
	mOpenLog        *systray.MenuItem
	mPrivacyNotice  *systray.MenuItem
	mQuit           *systray.MenuItem
}

// Run starts the systray loop and blocks until Quit is clicked.
func Run(cfg *config.Config, cfgPath string, useInProcess bool, logf func(string, ...any)) {
	var backend Backend
	if useInProcess || config.IsFlatpak() || os.Getenv("MYOVPN_IN_PROCESS") == "1" {
		backend = NewInProcessBackend(cfg, cfgPath, logf)
	} else {
		backend = &IPCBackend{}
	}

	app := &App{
		cfgPath: cfgPath,
		cfg:     cfg,
		backend: backend,
		logf:    logf,
		page:    1,
		stopCh:  make(chan struct{}),
	}
	systray.Run(app.onReady, app.onExit)
}

func (a *App) onReady() {
	systray.SetIcon(assets.TrayStateIcon("white"))
	systray.SetTooltip("MyOVPN — Not connected")

	_ = autostart.Sync(a.cfg.Autostart)
	_ = autostart.InstallChromeVPNLauncher()

	a.buildMenu()

	if !a.cfg.FirstRunNoticeShown {
		notify.Send(
			"MyOVPN — Privacy Notice",
			"Free public VPN servers change your country but are run by volunteers. Use for geo-access, stick to HTTPS sites, and never do banking through them.",
		)
		a.cfg.FirstRunNoticeShown = true
		_ = config.Save(a.cfgPath, a.cfg)
	}

	// Initial background refresh (never auto-connects on startup per FR-17)
	go func() {
		_, _ = a.backend.Refresh(false)
		a.pollOnce()
	}()

	go a.eventLoop()
	go a.pollLoop()
	go a.blinkLoop()
}

func (a *App) onExit() {
	close(a.stopCh)
	_ = a.backend.Quit()
}

func (a *App) buildMenu() {
	a.mStatus = systray.AddMenuItem("○ Not connected", "Current connection state")
	a.mStatus.Disable()
	systray.AddSeparator()

	a.mChooseCountry = systray.AddMenuItem("Choose country", "Connect to the best server in a country")
	for i := 0; i < countriesPerPage; i++ {
		slot := a.mChooseCountry.AddSubMenuItem("Loading…", "")
		slot.Hide()
		a.mCountrySlots[i] = slot
	}
	a.mChooseCountry.AddSubMenuItem("─────────", "").Disable()
	a.mPrevPage = a.mChooseCountry.AddSubMenuItem("‹ Previous", "Previous country page")
	a.mPageLabel = a.mChooseCountry.AddSubMenuItem("Page 1 / 1", "")
	a.mPageLabel.Disable()
	a.mNextPage = a.mChooseCountry.AddSubMenuItem("Next ›", "Next country page")
	a.mCacheNotice = a.mChooseCountry.AddSubMenuItem("", "Cached server list")
	a.mCacheNotice.Disable()
	a.mCacheNotice.Hide()
	a.mRefresh = a.mChooseCountry.AddSubMenuItem("🔄 Refresh list", "Fetch fresh server list")

	a.mServerMenu = systray.AddMenuItem("Server", "Pick a specific candidate server")
	for i := 0; i < len(a.mServerSlots); i++ {
		slot := a.mServerMenu.AddSubMenuItem("", "")
		slot.Hide()
		a.mServerSlots[i] = slot
	}

	a.mNextServer = systray.AddMenuItem("Next server", "Switch to next candidate server")
	a.mLaunchChrome = systray.AddMenuItem("Launch Chrome (VPN)", "Open isolated Chrome inside the VPN namespace")
	if runtime.GOOS == "windows" || config.IsFlatpak() {
		a.mLaunchChrome.Hide()
	}
	a.mDisconnect = systray.AddMenuItem("Disconnect", "Stop the VPN tunnel")
	a.mDisconnect.Disable()

	systray.AddSeparator()
	a.mSettings = systray.AddMenuItem("Settings", "Configure MyOVPN")

	a.mModePerApp = a.mSettings.AddSubMenuItemCheckbox("Mode: Per-app (Chrome only)", "Route only launched apps through VPN", a.cfg.Mode == "per-app")
	a.mModeSystemWide = a.mSettings.AddSubMenuItemCheckbox("Mode: System-wide", "Route all machine traffic through VPN", a.cfg.Mode == "system-wide")
	if runtime.GOOS == "windows" || config.IsFlatpak() {
		a.mModePerApp.Disable()
	}
	a.mStartAtLogin = a.mSettings.AddSubMenuItemCheckbox("Start at login", "Load tray icon automatically at login (never auto-connects)", a.cfg.Autostart)
	a.mNotifications = a.mSettings.AddSubMenuItemCheckbox("Notifications", "Show connection and failover notifications", a.cfg.Notifications)
	a.mAutoLaunch = a.mSettings.AddSubMenuItemCheckbox("Auto-launch Chrome on connect", "Open Chrome (VPN) automatically when per-app VPN connects", a.cfg.AutoLaunchApp)
	if runtime.GOOS == "windows" || config.IsFlatpak() {
		a.mAutoLaunch.Hide()
	}

	a.mEnablePerApp = a.mSettings.AddSubMenuItem("Enable per-app mode…", "Install root helper service for per-app namespaces")
	if !config.IsAppImage() {
		a.mEnablePerApp.Hide()
	}
	a.mRemoveHelper = a.mSettings.AddSubMenuItem("Remove helper service", "Uninstall the Windows helper service")
	if runtime.GOOS != "windows" {
		a.mRemoveHelper.Hide()
	}

	a.mEditSettings = a.mSettings.AddSubMenuItem("Edit settings file…", "Open config.toml")
	a.mOpenLog = a.mSettings.AddSubMenuItem("Open log", "View rotating MyOVPN logs")
	a.mPrivacyNotice = a.mSettings.AddSubMenuItem("Privacy notice", "Show security & privacy reminder")

	a.mQuit = systray.AddMenuItem("Quit", "Disconnect and clean up profiles/namespace")
}

func (a *App) pollLoop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-a.stopCh:
			return
		case <-t.C:
			a.pollOnce()
		}
	}
}

func (a *App) pollOnce() {
	snap, _ := a.backend.Status()
	a.applyStatus(snap)
}

func (a *App) blinkLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	showGreen := false
	for {
		select {
		case <-a.stopCh:
			return
		case <-t.C:
			a.mu.Lock()
			st := a.snap.State
			a.mu.Unlock()
			if st == core.StateConnecting {
				showGreen = !showGreen
				if showGreen {
					systray.SetIcon(assets.TrayStateIcon("green"))
				} else {
					systray.SetIcon(assets.TrayStateIcon("white"))
				}
			}
		}
	}
}

func (a *App) applyStatus(snap core.StatusSnapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.snap = snap

	if snap.Notice != nil && snap.Notice.Seq > a.lastNoticeSeq {
		isInitial := a.lastNoticeSeq == 0
		a.lastNoticeSeq = snap.Notice.Seq
		if !isInitial && a.cfg.Notifications {
			go notify.Send(snap.Notice.Title, snap.Notice.Body)
		}
	}

	// Auto-launch Chrome (VPN) on transition into Connected in per-app mode
	if snap.State == core.StateConnected && a.prevState != core.StateConnected {
		if a.cfg.AutoLaunchApp && snap.Mode == "per-app" && !a.autoLaunched && runtime.GOOS == "linux" {
			a.autoLaunched = true
			go func() {
				_ = a.backend.LaunchApp("chrome")
			}()
		}
	}
	if snap.State == core.StateDisconnected {
		a.autoLaunched = false
	}
	a.prevState = snap.State

	switch snap.State {
	case core.StateConnected:
		systray.SetIcon(assets.TrayStateIcon("green"))
		if snap.ActiveServer != nil {
			s := snap.ActiveServer
			line := fmt.Sprintf("● Connected — %s · %s · %s · %s",
				s.CountryName, s.IP, s.FormatSpeed(), s.FormatSessions())
			a.mStatus.SetTitle(line)
			systray.SetTooltip(fmt.Sprintf("MyOVPN — Connected (%s · %s · %s)", s.CountryName, s.IP, s.FormatSpeed()))
		} else {
			a.mStatus.SetTitle("● Connected")
			systray.SetTooltip("MyOVPN — Connected")
		}
		a.mDisconnect.Enable()
		a.mNextServer.Enable()
	case core.StateConnecting:
		name := snap.CountryName
		if name == "" {
			name = snap.CountryCode
		}
		a.mStatus.SetTitle(fmt.Sprintf("◌ Connecting — %s…", name))
		systray.SetTooltip(fmt.Sprintf("MyOVPN — Connecting to %s", name))
		a.mDisconnect.Enable()
		a.mNextServer.Enable()
	case core.StateError:
		systray.SetIcon(assets.TrayStateIcon("red"))
		errTxt := snap.LastError
		if errTxt == "" {
			errTxt = "Unknown error"
		}
		a.mStatus.SetTitle("✖ Error — " + errTxt)
		systray.SetTooltip("MyOVPN — Error: " + errTxt)
		a.mDisconnect.Disable()
		a.mNextServer.Disable()
	default:
		systray.SetIcon(assets.TrayStateIcon("white"))
		a.mStatus.SetTitle("○ Not connected")
		systray.SetTooltip("MyOVPN — Not connected")
		a.mDisconnect.Disable()
		a.mNextServer.Disable()
	}

	// Update mode checkboxes
	if snap.Mode == "per-app" {
		a.mModePerApp.Check()
		a.mModeSystemWide.Uncheck()
		if runtime.GOOS == "linux" && !config.IsFlatpak() {
			a.mLaunchChrome.Show()
		}
	} else if snap.Mode == "system-wide" {
		a.mModePerApp.Uncheck()
		a.mModeSystemWide.Check()
	}

	// Update Windows helper menu visibility
	if runtime.GOOS == "windows" {
		if winservice.IsInstalled() {
			a.mRemoveHelper.Show()
		} else {
			a.mRemoveHelper.Hide()
		}
	}

	// Cache banner
	if snap.UsingCache {
		age := snap.CacheAge
		if age == "" {
			age = "offline"
		}
		a.mCacheNotice.SetTitle(fmt.Sprintf("⚠ Using cached list (%s old)", age))
		a.mCacheNotice.Show()
	} else if snap.SourceUsed != "" {
		a.mCacheNotice.SetTitle("Source: " + snap.SourceUsed)
		a.mCacheNotice.Show()
	} else {
		a.mCacheNotice.Hide()
	}

	// Update country pagination (10 per page)
	pageItems, curPage, totalPages := core.PaginateCountries(snap.Countries, a.page, countriesPerPage)
	a.page = curPage
	a.pageCountries = pageItems
	a.mPageLabel.SetTitle(fmt.Sprintf("Page %d / %d", curPage, totalPages))
	if curPage <= 1 {
		a.mPrevPage.Disable()
	} else {
		a.mPrevPage.Enable()
	}
	if curPage >= totalPages {
		a.mNextPage.Disable()
	} else {
		a.mNextPage.Enable()
	}

	for i := 0; i < countriesPerPage; i++ {
		if i < len(pageItems) {
			c := pageItems[i]
			pin := ""
			if c.Pinned {
				pin = " ★"
			}
			a.mCountrySlots[i].SetTitle(fmt.Sprintf("%s %s (%d)%s", c.Flag, c.Name, c.ServerCount, pin))
			if strings.EqualFold(c.Code, snap.CountryCode) && snap.State != core.StateDisconnected {
				a.mCountrySlots[i].Check()
			} else {
				a.mCountrySlots[i].Uncheck()
			}
			a.mCountrySlots[i].Show()
		} else {
			a.mCountrySlots[i].Hide()
		}
	}

	// Update candidate servers submenu
	a.candidates = snap.Candidates
	if len(snap.Candidates) == 0 {
		a.mServerMenu.Disable()
	} else {
		a.mServerMenu.Enable()
	}
	for i := 0; i < len(a.mServerSlots); i++ {
		if i < len(snap.Candidates) {
			s := snap.Candidates[i]
			lock := ""
			if s.StrongCipher {
				lock = "🔒 "
			}
			tag := ""
			if s.SourceTag != "" {
				tag = " [" + s.SourceTag + "]"
			}
			title := fmt.Sprintf("%d. %s%s — %s, %s%s",
				i+1, lock, s.IP, s.FormatSpeed(), s.FormatSessions(), tag)
			a.mServerSlots[i].SetTitle(title)
			if snap.ActiveServer != nil && snap.ActiveServer.ID == s.ID {
				a.mServerSlots[i].Check()
			} else {
				a.mServerSlots[i].Uncheck()
			}
			a.mServerSlots[i].Show()
		} else {
			a.mServerSlots[i].Hide()
		}
	}
}

func (a *App) eventLoop() {
	for i := 0; i < countriesPerPage; i++ {
		idx := i
		go func() {
			for {
				select {
				case <-a.stopCh:
					return
				case <-a.mCountrySlots[idx].ClickedCh:
					a.mu.Lock()
					if idx < len(a.pageCountries) {
						cc := a.pageCountries[idx].Code
						a.mu.Unlock()
						if err := a.backend.Connect(cc, ""); err != nil {
							notify.Send("MyOVPN Error", err.Error())
						}
						a.pollOnce()
					} else {
						a.mu.Unlock()
					}
				}
			}
		}()
	}

	for i := 0; i < len(a.mServerSlots); i++ {
		idx := i
		go func() {
			for {
				select {
				case <-a.stopCh:
					return
				case <-a.mServerSlots[idx].ClickedCh:
					a.mu.Lock()
					if idx < len(a.candidates) {
						srv := a.candidates[idx]
						a.mu.Unlock()
						_ = a.backend.Connect(srv.CountryCode, srv.ID)
						a.pollOnce()
					} else {
						a.mu.Unlock()
					}
				}
			}
		}()
	}

	for {
		select {
		case <-a.stopCh:
			return
		case <-a.mPrevPage.ClickedCh:
			a.mu.Lock()
			if a.page > 1 {
				a.page--
			}
			snap := a.snap
			a.mu.Unlock()
			a.applyStatus(snap)
		case <-a.mNextPage.ClickedCh:
			a.mu.Lock()
			a.page++
			snap := a.snap
			a.mu.Unlock()
			a.applyStatus(snap)
		case <-a.mRefresh.ClickedCh:
			go func() {
				snap, _ := a.backend.Refresh(true)
				a.applyStatus(snap)
			}()
		case <-a.mNextServer.ClickedCh:
			go func() {
				_ = a.backend.NextServer()
				a.pollOnce()
			}()
		case <-a.mLaunchChrome.ClickedCh:
			go func() {
				if err := a.backend.LaunchApp("chrome"); err != nil {
					notify.Send("MyOVPN", "Could not launch Chrome (VPN): "+err.Error())
				}
			}()
		case <-a.mDisconnect.ClickedCh:
			go func() {
				_ = a.backend.Disconnect()
				a.pollOnce()
			}()
		case <-a.mModePerApp.ClickedCh:
			go func() {
				a.cfg.Mode = "per-app"
				_ = config.Save(a.cfgPath, a.cfg)
				_ = a.backend.SetMode("per-app")
				a.pollOnce()
			}()
		case <-a.mModeSystemWide.ClickedCh:
			go func() {
				a.cfg.Mode = "system-wide"
				_ = config.Save(a.cfgPath, a.cfg)
				_ = a.backend.SetMode("system-wide")
				a.pollOnce()
			}()
		case <-a.mStartAtLogin.ClickedCh:
			a.cfg.Autostart = !a.cfg.Autostart
			if a.cfg.Autostart {
				a.mStartAtLogin.Check()
			} else {
				a.mStartAtLogin.Uncheck()
			}
			_ = config.Save(a.cfgPath, a.cfg)
			_ = autostart.Sync(a.cfg.Autostart)
		case <-a.mNotifications.ClickedCh:
			a.cfg.Notifications = !a.cfg.Notifications
			if a.cfg.Notifications {
				a.mNotifications.Check()
			} else {
				a.mNotifications.Uncheck()
			}
			_ = config.Save(a.cfgPath, a.cfg)
			_ = a.backend.ReloadConfig()
		case <-a.mAutoLaunch.ClickedCh:
			a.cfg.AutoLaunchApp = !a.cfg.AutoLaunchApp
			if a.cfg.AutoLaunchApp {
				a.mAutoLaunch.Check()
			} else {
				a.mAutoLaunch.Uncheck()
			}
			_ = config.Save(a.cfgPath, a.cfg)
			_ = a.backend.ReloadConfig()
		case <-a.mEnablePerApp.ClickedCh:
			go func() {
				if err := installHelperElevated(); err != nil {
					notify.Send("MyOVPN", "Helper install failed: "+err.Error())
				} else {
					notify.Send("MyOVPN", "Per-app helper service installed and running.")
					a.pollOnce()
				}
			}()
		case <-a.mRemoveHelper.ClickedCh:
			go func() {
				if err := removeHelperElevated(); err != nil {
					notify.Send("MyOVPN", "Remove helper failed: "+err.Error())
				} else {
					notify.Send("MyOVPN", "MyOVPN Helper service removed.")
					a.pollOnce()
				}
			}()
		case <-a.mEditSettings.ClickedCh:
			_ = config.Save(a.cfgPath, a.cfg)
			openPath(a.cfgPath)
		case <-a.mOpenLog.ClickedCh:
			openPath(config.LogPath())
		case <-a.mPrivacyNotice.ClickedCh:
			notify.Send(
				"MyOVPN — Privacy Notice",
				"Free public servers change your country; they don't make you private. Use for geo-access, stick to HTTPS sites, and never do banking through them. Note: apps already running outside the VPN namespace cannot be moved into it.",
			)
		case <-a.mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}
