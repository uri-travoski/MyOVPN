package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/controller"
	"github.com/uri-travoski/MyOVPN/internal/core"
)

// Server handles fixed-command IPC requests and delegates to a Controller.
type Server struct {
	mu      sync.Mutex
	ctrl    *controller.Controller
	cfg     *config.Config
	cfgPath string
	logf    func(string, ...any)
	bound   bool
}

func NewServer(ctrl *controller.Controller, cfg *config.Config, cfgPath string, logf func(string, ...any)) *Server {
	return &Server{ctrl: ctrl, cfg: cfg, cfgPath: cfgPath, logf: logf}
}

// Serve accepts connections on ln until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	caller, err := PeerCaller(conn)
	if err != nil {
		_ = writeResp(conn, Response{OK: false, Error: err.Error()})
		return
	}

	// Bind daemon config/cache to the desktop user on first user request (Linux root helper)
	s.bindCallerConfig(caller)

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 16*1024), 512*1024)
	if !sc.Scan() {
		return
	}
	var req Request
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		_ = writeResp(conn, Response{OK: false, Error: "invalid request json"})
		return
	}

	resp := s.dispatch(ctx, caller, req)
	_ = writeResp(conn, resp)
}

func (s *Server) bindCallerConfig(caller CallerInfo) {
	if runtime.GOOS != "linux" || caller.UID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bound {
		return
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(caller.UID), 10))
	if err != nil || u.HomeDir == "" {
		return
	}
	userCfgPath := filepath.Join(u.HomeDir, ".config", config.BinaryName, "config.toml")
	if info, err := os.Stat(userCfgPath); err == nil && info != nil {
		if cfg, err := config.Load(userCfgPath); err == nil {
			s.cfg = cfg
			s.cfgPath = userCfgPath
			s.ctrl.ReloadConfig(cfg)
		}
	}
	s.bound = true
}

func (s *Server) dispatch(ctx context.Context, caller CallerInfo, req Request) Response {
	switch req.Command {
	case CmdStatus:
		snap := s.ctrl.Status()
		return Response{OK: true, Status: &snap}

	case CmdRefresh:
		snap := s.ctrl.Refresh(ctx, req.Force)
		return Response{OK: true, Status: &snap}

	case CmdConnect:
		if err := s.ctrl.Connect(ctx, req.CountryCode, req.ServerID); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		snap := s.ctrl.Status()
		return Response{OK: true, Status: &snap}

	case CmdNext:
		if err := s.ctrl.NextServer(ctx); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		snap := s.ctrl.Status()
		return Response{OK: true, Status: &snap}

	case CmdDisconnect:
		if err := s.ctrl.Disconnect(ctx); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		snap := s.ctrl.Status()
		return Response{OK: true, Status: &snap}

	case CmdLaunch:
		// Refresh user config on launch so newly added [[apps]] entries work immediately
		if s.cfgPath != "" {
			if cfg, err := config.Load(s.cfgPath); err == nil {
				s.ctrl.ReloadConfig(cfg)
			}
		}
		env := FilterSessionEnv(req.SessionEnv)
		if err := s.ctrl.LaunchApp(caller.UID, caller.GID, env, req.AppID); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true}

	case CmdCleanup:
		if err := s.ctrl.Quit(ctx); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true}

	case CmdSetMode:
		if err := s.ctrl.SetMode(ctx, req.Mode); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		snap := s.ctrl.Status()
		return Response{OK: true, Status: &snap}

	case CmdReloadConfig:
		if s.cfgPath != "" {
			if cfg, err := config.Load(s.cfgPath); err == nil {
				s.ctrl.ReloadConfig(cfg)
			}
		}
		snap := s.ctrl.Status()
		return Response{OK: true, Status: &snap}

	case CmdQuit:
		if err := s.ctrl.Quit(ctx); err != nil {
			return Response{OK: false, Error: err.Error()}
		}
		return Response{OK: true}

	default:
		return Response{OK: false, Error: "unknown command"}
	}
}

func writeResp(conn net.Conn, resp Response) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err
}

// Call sends a single fixed-command request to the running helper daemon.
func Call(req Request) (*Response, error) {
	conn, err := Dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if _, err := conn.Write(b); err != nil {
		return nil, err
	}

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, err
		}
		return nil, net.ErrClosed
	}
	var resp Response
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return nil, err
	}
	if !resp.OK && resp.Error != "" {
		return &resp, &RemoteError{Msg: resp.Error}
	}
	return &resp, nil
}

// RemoteError wraps an error message returned by the helper daemon.
type RemoteError struct {
	Msg string
}

func (e *RemoteError) Error() string { return e.Msg }

// CurrentSessionEnv captures safe GUI environment variables from the user session.
func CurrentSessionEnv() map[string]string {
	out := make(map[string]string, len(AllowedSessionEnvKeys))
	for _, k := range AllowedSessionEnvKeys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			out[k] = v
		}
	}
	return out
}

// StatusOrNil queries status and returns nil if helper is unreachable.
func StatusOrNil() (*core.StatusSnapshot, error) {
	resp, err := Call(Request{Command: CmdStatus})
	if err != nil {
		return nil, err
	}
	return resp.Status, nil
}
