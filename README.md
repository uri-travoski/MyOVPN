# MyOVPN

Lightweight, one-click free OpenVPN tray app for **Linux** and **Windows**, with a companion **Android** app.

- **Two clicks to connect:** click tray icon -> pick country -> connects to the highest-scoring server.
- **Automatic failover:** health-checks `https://www.gstatic.com/generate_204` every 20 seconds from inside the tunnel; switches to the next candidate on 2 consecutive failures and blacklists dead IPs for 60 minutes.
- **Per-app routing (Linux default):** isolates `openvpn` and launched apps (e.g. `Chrome (VPN)` with its own profile and `--force-webrtc-ip-handling-policy=disable_non_proxied_udp`) inside the `myovpn` network namespace with an `nftables` kill switch and IPv6 leak block. All other apps (e.g. Firefox) keep using your normal connection.
- **System-wide mode:** imports and manages at most 3 `myovpn-*` NetworkManager profiles on Linux (without touching other profiles), or runs `openvpn.exe` via the `MyOVPNHelper` Windows service.
- **Multi-source server discovery:** VPN Gate API, VPN Gate proxy mirrors, VPNBook, local `.ovpn` imports (e.g. Proton VPN Free / own VPS), custom URLs, and 15-minute local cache fallback.
- **Android app:** Kotlin + `VpnService` (`addAllowedApplication` per-app allowlist, defaulting to Chrome) + Quick Settings tile + in-app log viewer.

## Status Colours

- **White** (dark-outlined): running, not connected (startup default; never connects automatically at login).
- **Blinking white/green**: connecting or failing over.
- **Green**: connected and passing tunnel health checks.
- **Red**: error (all sources unreachable, no working servers after retries, helper stopped, or missing Windows VPN driver).

## Security & Privacy Notice

Free public VPN servers are operated by volunteers who can observe traffic metadata. Use MyOVPN for geo-access, stick to HTTPS websites, and do not perform banking or sensitive logins through public servers.

Every downloaded `.ovpn` config is sanitized before use (`up`, `down`, `script-security`, `plugin`, `route-up`, `ipchange`, `tls-verify`, etc. are stripped and `remote` is validated against the listed server).

## Building & Testing

### Desktop (Linux & Windows)

```bash
go test -v ./...
CGO_ENABLED=0 go build -o myovpn ./cmd/myovpn
./myovpn --version
```

Cross-compile Windows GUI binaries:

```bash
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui" -o myovpn-x64.exe ./cmd/myovpn
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -ldflags "-H=windowsgui" -o myovpn-arm64.exe ./cmd/myovpn
```

### Android

```bash
cd android
./gradlew assembleDebug
```

## Release Builds

Tagging `v1.1.0` (or triggering `workflow_dispatch`) runs `.github/workflows/release.yml` on native Linux (`ubuntu-24.04`, `ubuntu-24.04-arm`) and Windows (`windows-latest`, `windows-11-arm`) runners to build `.deb`, `.rpm`, `.AppImage`, `.flatpak`, Windows Setup `.exe`, portable `.zip`, Android `.apk`, and `SHA256SUMS.txt`.

## License

GPL-3.0-or-later (see [LICENSE](LICENSE)).
