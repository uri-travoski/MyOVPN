package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/controller"
	"github.com/uri-travoski/MyOVPN/internal/ipc"
	"github.com/uri-travoski/MyOVPN/internal/logutil"
	"github.com/uri-travoski/MyOVPN/internal/netns"
	"github.com/uri-travoski/MyOVPN/internal/nm"
	"github.com/uri-travoski/MyOVPN/internal/power"
	"github.com/uri-travoski/MyOVPN/internal/tray"
	"github.com/uri-travoski/MyOVPN/internal/version"
	"github.com/uri-travoski/MyOVPN/internal/winservice"
)

func nilCtx() context.Context { return context.Background() }

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Printf("MyOVPN %s (%s/%s)\n", version.Version, runtime.GOOS, runtime.GOARCH)
			return
		case "--help", "-h", "help":
			printUsage()
			return
		case "_netns-http":
			os.Exit(runNetnsHTTP(os.Args[2:]))
		case "_netns-launch":
			os.Exit(runNetnsLaunch(os.Args[2:]))
		case "daemon":
			os.Exit(runDaemonCommand())
		case "install-service":
			if err := installService(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "uninstall-service":
			if err := uninstallService(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "cleanup":
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = netns.CleanupAll(ctx)
			_ = nm.NewManager(controller.DefaultRegistryPath(), 3).CleanupAll(ctx)
			return
		case "status", "list", "refresh", "connect", "next", "disconnect", "launch", "set-mode", "quit":
			os.Exit(runCLI(os.Args[1:]))
		}
	}

	// Default: if launched as Windows Service by SCM, run daemon; otherwise run tray UI
	if winservice.IsServiceProcess() {
		os.Exit(runDaemonCommand())
	}

	cfgPath := config.ConfigPath()
	cfg, _ := config.Load(cfgPath)
	logger, rw, _ := logutil.SetupLogger(config.LogPath(), "[tray] ", false)
	if rw != nil {
		defer rw.Close()
	}

	tray.Run(cfg, cfgPath, false, logger.Printf)
}

func runDaemonCommand() int {
	if winservice.IsServiceProcess() {
		if err := winservice.Run(runDaemonLoop); err != nil {
			fmt.Fprintln(os.Stderr, "service run:", err)
			return 1
		}
		return 0
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := runDaemonLoop(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "daemon:", err)
		return 1
	}
	return 0
}

func runDaemonLoop(ctx context.Context) error {
	logger, rw, _ := logutil.SetupLogger(config.DaemonLogPath(), "[myovpnd] ", true)
	if rw != nil {
		defer rw.Close()
	}

	cfgPath := config.ConfigPath()
	cfg, _ := config.Load(cfgPath)
	cachePath := config.CacheServersPath()
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		cachePath = "/var/cache/myovpn/servers.json"
		_ = os.MkdirAll("/var/cache/myovpn", 0o755)
	}

	ctrl := controller.New(cfg, cfgPath, cachePath, controller.DefaultRegistryPath(), nil, logger.Printf)
	ctrl.StartupCleanup(ctx)

	ln, err := ipc.Listen()
	if err != nil {
		return fmt.Errorf("ipc listen: %w", err)
	}
	defer ln.Close()
	logger.Printf("MyOVPN helper %s listening", version.Version)

	go power.WatchResumeAndNetwork(ctx, func(reason string) {
		logger.Printf("power/network event: %s", reason)
		ctrl.TriggerHealthCheck()
	})

	srv := ipc.NewServer(ctrl, cfg, cfgPath, logger.Printf)
	err = srv.Serve(ctx, ln)
	_ = ctrl.Quit(context.Background())
	return err
}

func runCLI(args []string) int {
	cmd := args[0]
	var req ipc.Request

	switch cmd {
	case "status":
		req = ipc.Request{Command: ipc.CmdStatus}
	case "refresh":
		req = ipc.Request{Command: ipc.CmdRefresh, Force: true}
	case "list":
		req = ipc.Request{Command: ipc.CmdStatus}
	case "connect":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: myovpn connect <COUNTRY_CODE> [SERVER_ID]")
			return 2
		}
		req = ipc.Request{Command: ipc.CmdConnect, CountryCode: strings.ToUpper(args[1])}
		if len(args) >= 3 {
			req.ServerID = args[2]
		}
	case "next":
		req = ipc.Request{Command: ipc.CmdNext}
	case "disconnect":
		req = ipc.Request{Command: ipc.CmdDisconnect}
	case "launch":
		appID := "chrome"
		if len(args) >= 2 {
			appID = args[1]
		}
		req = ipc.Request{Command: ipc.CmdLaunch, AppID: appID, SessionEnv: ipc.CurrentSessionEnv()}
	case "set-mode":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: myovpn set-mode <per-app|system-wide>")
			return 2
		}
		req = ipc.Request{Command: ipc.CmdSetMode, Mode: args[1]}
	case "quit":
		req = ipc.Request{Command: ipc.CmdQuit}
	}

	resp, err := ipc.Call(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	if cmd == "list" && resp.Status != nil {
		for _, c := range resp.Status.Countries {
			fmt.Printf("%s %-4s %-24s (%d)\n", c.Flag, c.Code, c.Name, c.ServerCount)
		}
		return 0
	}

	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
	return 0
}

func printUsage() {
	fmt.Printf(`MyOVPN %s — One-click free OpenVPN tray app

Usage:
  myovpn                  Start the system tray app
  myovpn daemon           Run the privileged helper service
  myovpn status           Show current connection status
  myovpn list             List countries with usable servers
  myovpn refresh          Refresh server list from sources
  myovpn connect <CC>     Connect to top server in country (e.g. JP)
  myovpn next             Switch to next candidate server
  myovpn disconnect       Stop active tunnel
  myovpn launch [app-id]  Launch app inside VPN namespace (default: chrome)
  myovpn set-mode <mode>  Switch between per-app and system-wide
  myovpn cleanup          Remove all myovpn-* profiles and namespace
  myovpn --version        Print version
`, version.Version)
}
