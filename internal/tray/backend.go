package tray

import (
	"context"
	"runtime"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/controller"
	"github.com/uri-travoski/MyOVPN/internal/core"
	"github.com/uri-travoski/MyOVPN/internal/ipc"
	"github.com/uri-travoski/MyOVPN/internal/notify"
	"github.com/uri-travoski/MyOVPN/internal/winservice"
)

// Backend abstracts helper daemon IPC vs in-process Flatpak controller.
type Backend interface {
	Status() (core.StatusSnapshot, error)
	Refresh(force bool) (core.StatusSnapshot, error)
	Connect(countryCode, serverID string) error
	NextServer() error
	Disconnect() error
	LaunchApp(appID string) error
	SetMode(mode string) error
	ReloadConfig() error
	Quit() error
}

// IPCBackend communicates with `myovpnd` / `MyOVPNHelper`.
type IPCBackend struct{}

func (b *IPCBackend) Status() (core.StatusSnapshot, error) {
	resp, err := ipc.Call(ipc.Request{Command: ipc.CmdStatus})
	if err != nil {
		return core.StatusSnapshot{
			State:         core.StateError,
			LastError:     "Helper service not running",
			HelperRunning: false,
		}, err
	}
	if resp.Status == nil {
		return core.StatusSnapshot{}, nil
	}
	return *resp.Status, nil
}

func (b *IPCBackend) Refresh(force bool) (core.StatusSnapshot, error) {
	resp, err := ipc.Call(ipc.Request{Command: ipc.CmdRefresh, Force: force})
	if err != nil {
		return core.StatusSnapshot{}, err
	}
	if resp.Status == nil {
		return core.StatusSnapshot{}, nil
	}
	return *resp.Status, nil
}

func (b *IPCBackend) Connect(countryCode, serverID string) error {
	_, err := ipc.Call(ipc.Request{
		Command:     ipc.CmdConnect,
		CountryCode: countryCode,
		ServerID:    serverID,
	})
	if err != nil && runtime.GOOS == "windows" && !winservice.IsInstalled() {
		// Windows portable mode: prompt UAC once on first connect to install driver/service
		if uacErr := winservice.ElevateAndRun("install-service"); uacErr == nil {
			time.Sleep(2 * time.Second)
			_, err = ipc.Call(ipc.Request{
				Command:     ipc.CmdConnect,
				CountryCode: countryCode,
				ServerID:    serverID,
			})
		}
	}
	return err
}

func (b *IPCBackend) NextServer() error {
	_, err := ipc.Call(ipc.Request{Command: ipc.CmdNext})
	return err
}

func (b *IPCBackend) Disconnect() error {
	_, err := ipc.Call(ipc.Request{Command: ipc.CmdDisconnect})
	return err
}

func (b *IPCBackend) LaunchApp(appID string) error {
	_, err := ipc.Call(ipc.Request{
		Command:    ipc.CmdLaunch,
		AppID:      appID,
		SessionEnv: ipc.CurrentSessionEnv(),
	})
	return err
}

func (b *IPCBackend) SetMode(mode string) error {
	_, err := ipc.Call(ipc.Request{Command: ipc.CmdSetMode, Mode: mode})
	return err
}

func (b *IPCBackend) ReloadConfig() error {
	_, err := ipc.Call(ipc.Request{Command: ipc.CmdReloadConfig})
	return err
}

func (b *IPCBackend) Quit() error {
	_, err := ipc.Call(ipc.Request{Command: ipc.CmdQuit})
	return err
}

// InProcessBackend runs Controller directly inside the tray process (used in Flatpak or standalone mode).
type InProcessBackend struct {
	ctrl *controller.Controller
	cfg  *config.Config
	path string
}

type trayEventSink struct{}

func (s trayEventSink) OnNotify(title, body string) { notify.Send(title, body) }
func (s trayEventSink) OnStatus(_ core.StatusSnapshot) {}

func NewInProcessBackend(cfg *config.Config, cfgPath string, logf func(string, ...any)) *InProcessBackend {
	ctrl := controller.New(cfg, cfgPath, config.CacheServersPath(), controller.DefaultRegistryPath(), trayEventSink{}, logf)
	ctrl.StartupCleanup(context.Background())
	return &InProcessBackend{ctrl: ctrl, cfg: cfg, path: cfgPath}
}

func (b *InProcessBackend) Status() (core.StatusSnapshot, error) {
	return b.ctrl.Status(), nil
}

func (b *InProcessBackend) Refresh(force bool) (core.StatusSnapshot, error) {
	return b.ctrl.Refresh(context.Background(), force), nil
}

func (b *InProcessBackend) Connect(countryCode, serverID string) error {
	return b.ctrl.Connect(context.Background(), countryCode, serverID)
}

func (b *InProcessBackend) NextServer() error {
	return b.ctrl.NextServer(context.Background())
}

func (b *InProcessBackend) Disconnect() error {
	return b.ctrl.Disconnect(context.Background())
}

func (b *InProcessBackend) LaunchApp(appID string) error {
	return b.ctrl.LaunchApp(0, 0, ipc.FilterSessionEnv(ipc.CurrentSessionEnv()), appID)
}

func (b *InProcessBackend) SetMode(mode string) error {
	return b.ctrl.SetMode(context.Background(), mode)
}

func (b *InProcessBackend) ReloadConfig() error {
	if cfg, err := config.Load(b.path); err == nil {
		b.ctrl.ReloadConfig(cfg)
	}
	return nil
}

func (b *InProcessBackend) Quit() error {
	return b.ctrl.Quit(context.Background())
}
