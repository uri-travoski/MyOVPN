//go:build linux

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/health"
)

func runNetnsHTTP(args []string) int {
	fs := flag.NewFlagSet("_netns-http", flag.ExitOnError)
	targetURL := fs.String("url", "", "URL to fetch")
	timeoutStr := fs.String("timeout", "8s", "Request timeout")
	printBody := fs.Bool("body", false, "Print response body")
	_ = fs.Parse(args)

	if *targetURL == "" {
		return 2
	}
	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		timeout = 8 * time.Second
	}
	if *printBody {
		body, err := health.DoHTTPGetBody(nilCtx(), *targetURL, timeout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(body)
		return 0
	}
	if err := health.DoHTTPGet(nilCtx(), *targetURL, timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runNetnsLaunch(args []string) int {
	fs := flag.NewFlagSet("_netns-launch", flag.ExitOnError)
	uid := fs.Uint("uid", 0, "Target UID")
	gid := fs.Uint("gid", 0, "Target GID")
	cmdName := fs.String("cmd", "", "Command to launch")
	_ = fs.Parse(args)
	cmdArgs := fs.Args()

	if *cmdName == "" || *uid == 0 {
		fmt.Fprintln(os.Stderr, "refusing to launch without non-root uid and cmd")
		return 2
	}

	// Drop root privileges permanently to the logged-in user (FR-14)
	if err := syscall.Setgid(int(*gid)); err != nil {
		fmt.Fprintln(os.Stderr, "setgid:", err)
		return 1
	}
	if err := syscall.Setuid(int(*uid)); err != nil {
		fmt.Fprintln(os.Stderr, "setuid:", err)
		return 1
	}

	binPath, err := exec.LookPath(*cmdName)
	if err != nil {
		// Common Linux fallback for Chromium/Chrome variants
		for _, alt := range []string{"chromium", "/usr/lib/chromium/chromium", "chromium-browser", "/usr/lib/chromium-browser/chromium-browser", "google-chrome-stable", "google-chrome"} {
			if p, e := exec.LookPath(alt); e == nil {
				binPath = p
				break
			}
		}
		if binPath == "" {
			fmt.Fprintln(os.Stderr, "command not found:", *cmdName)
			return 1
		}
	}

	argv := append([]string{binPath}, cmdArgs...)
	if err := syscall.Exec(binPath, argv, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "exec:", err)
		return 1
	}
	return 0
}
