# PRD — VPN Tray: one-click free OpenVPN by country (Linux, Windows, Android) — v1.1

Nov 25, 2026 · @Sam

## Summary

VPN Tray is a lightweight tray app for Linux and Windows, with an Android version. It loads in the tray at every login without connecting. You click its icon, pick a country, and it connects to the best free OpenVPN server there, switching to the next one on its own when a server dies. Only the apps you choose (for example Chrome) use the VPN; everything else (for example Firefox) keeps the normal connection.

**Problem today:** servers are picked by hand in a browser extension (Free OpenVPN Server Finder), .ovpn files are imported into NetworkManager one by one, and dead connections are deleted by hand every few hours. Volunteer servers drop often, so this repeats all day.

**Goals**

1. Two clicks to connect: tray icon, then country.
2. Automatic failover within 60 seconds of a server dying, with no user action.
3. Per-app routing: chosen apps use the VPN, all other apps don't.
4. No clutter: the app never leaves more than 3 of its own VPN profiles in NetworkManager, and never touches profiles it didn't create.
5. More than one server list source, so the app still works if VPN Gate is down or blocked.
6. Lightweight: under 60 MB RAM idle, no Electron, no browser engine.
7. Starts with the computer: the tray icon loads at every login, white and not connected, and never connects on its own.
8. Colour tells the state at a glance: white = not connected, green = connected, red = error.
9. One GitHub release, **v1.1**, built entirely by GitHub Actions: deb, rpm, AppImage and Flatpak for x86\_64 and arm64, a Windows installer and portable exe for x64 and arm64, and an Android APK.

**Non-goals:** not a privacy or anonymity tool (see Security), no macOS or iOS, no paid VPN provider accounts in v1.1, no WireGuard in v1.1.

## Routing modes

The default is **per-app mode**, built on a Linux network namespace. That is the reliable way to send Chrome through the VPN and keep Firefox off it. NetworkManager VPN profiles always route the whole machine, so they cannot do this. [vopono](https://github.com/jamesmcm/vopono) is a proven open-source tool that runs apps through a VPN this way; use it as the reference design, but do not depend on it.

|  | Per-app mode (default) | System-wide mode (option) |
| --- | --- | --- |
| What uses the VPN | Only apps launched from the tray (Chrome by default) | Every app on the machine |
| How | `openvpn` runs inside a namespace `vpntray`; apps are started inside it | `nmcli` imports the .ovpn and brings it up |
| Shows in Network settings | No | Yes, as `vpntray-<CC>-1..3` |
| Kill switch | Built in: if the tunnel drops, apps in the namespace have no internet. Nothing leaks | Optional, via NetworkManager |
| Firefox | Untouched | Also goes through the VPN |

**Per-app mode rules**

- The namespace contains only a veth link to the host (NAT'd), used solely to reach the VPN server's IP and port, plus `tun0` as the default route. nftables inside the namespace drops everything else, so traffic cannot bypass the tunnel.
- DNS for the namespace comes from `/etc/netns/vpntray/resolv.conf`, set to the DNS servers the VPN pushes, or 1.1.1.1 if it pushes none.
- Chrome must run as a **separate instance**: `google-chrome --user-data-dir=~/.config/vpntray/chrome-profile --class=ChromeVPN`. Without its own profile, a launch just opens a window in the already-running non-VPN Chrome. Show this Chrome with its own window class and a distinct profile colour so it is obvious which one is on the VPN.
- Switching servers restarts only `openvpn` inside the namespace. Chrome keeps running and reconnects by itself after a few seconds.
- The tray app installs a desktop launcher, "Chrome (VPN)", so the VPN browser can also be opened from the menu.
- Users can add more apps to the VPN list (path + arguments) in Settings. An app that is already running outside the namespace cannot be moved into it. Say this in the UI.

## User flows

Everything happens from the tray menu. There is no main window, only a small Settings dialog.

**Tray menu (connected state)**

```text
● Connected — Japan · 122.23.206.246 · 47 Mbps · 0 users   (disabled status line)
─────────────
Choose country  ▸   🇯🇵 Japan (84)
                    🇰🇷 Korea (61)
                    🇺🇸 United States (40)
                    … 10 per page …
                    ─────────
                    ‹ Previous   Page 1 / 6   Next ›
                    🔄 Refresh list
Server  ▸           ✓ 1. 122.23.206.246 — 47 Mbps, 0 users
                      2. 180.12.195.79 — 111 Mbps, 2 users
                      3. 218.224.147.2 — 45 Mbps, 1 user
Next server
Launch Chrome (VPN)
Disconnect
─────────────
Settings…
Quit
```

**Flows**

1. **Startup.** After every reboot or login, the tray icon loads by itself, white, not connected. The app never connects on its own at startup. Autostart is on by default and can be turned off.
2. **Connect.** Click the tray icon, open Choose country, click a country. The icon blinks while connecting, then turns green. A notification shows the country, IP and speed. In per-app mode, Chrome (VPN) opens automatically if that setting is on.
3. **Country list.** Show only countries with at least one usable server, sorted by number of usable servers (most first), 10 per page. Show each country's flag and server count. Remember the last 3 countries chosen and pin them at the top of page 1.
4. **Automatic failover.** When the current server fails its health check, the icon blinks and the app moves to the next candidate. A notification says "Switched to server 2 of 3 (reason: no traffic)". If all 3 candidates fail, the app fetches a fresh list and picks a new top 3 for the same country. It never changes country on its own. If nothing works, the icon turns red.
5. **Manual switch.** Pick any server from the Server submenu, or click Next server.
6. **Disconnect.** Stops the tunnel and the icon turns back to white. In system-wide mode it also brings the NetworkManager profile down, but keeps the 3 profiles for a fast reconnect. In per-app mode, Chrome (VPN) stays open but has no internet (kill switch).
7. **Quit.** Disconnects, deletes every `vpntray-*` profile, removes the namespace and removes the icon until the next login.

## Functional requirements

**List & selection**

- FR-1 Fetch server lists from the sources in the Sources section, in priority order. Cache the merged list for 15 minutes in `~/.cache/vpntray/servers.json`, and fall back to the cache when every source fails.
- FR-2 Normalise every source to one schema: `source, country_code, country_name, ip, port, proto, speed_bps, ping_ms, sessions, uptime_ms, ovpn_text, auth (none | user/pass)`. Leave a field null when the source doesn't provide it.
- FR-3 Rank servers per country with the score in the Ranking section and keep the top 3 as candidates.
- FR-4 Blacklist a server IP for 60 minutes after it fails, and skip it when picking candidates.

**Connection**

- FR-5 Connect to candidate 1. If it isn't connected within 20 seconds, or fails the first health check, move to 2, then 3, then fetch a fresh list (Ranking section).
- FR-6 Health check every 20 seconds: an HTTPS GET to `https://www.gstatic.com/generate_204` made from inside the tunnel (inside the namespace in per-app mode), 8-second timeout. Two failures in a row means the server is dead. Make the URL configurable.
- FR-7 After connecting, check once that the exit IP's country matches the chosen country (configurable geo-IP endpoint, off by default because it is a third-party service). A mismatch counts as a failure.
- FR-8 Manual Next server, pick a specific server, and Disconnect, all available at any time.
- FR-9 Handle OpenVPN 2.6 strictness with VPN Gate configs. They use `cipher AES-128-CBC` with `auth SHA1`, so add `data-ciphers AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305:AES-128-CBC` to each generated config. Never disable TLS or certificate checks to make a server connect.

**Cleanup (system-wide mode)**

- FR-10 Name every profile the app creates `vpntray-<CC>-<n>`, and tag it in the profile's `connection.id` and in a local registry file.
- FR-11 On country selection, on startup and on quit, delete every `vpntray-*` profile that isn't a current candidate. Never keep more than 3 (configurable 3–5). Never touch profiles without the prefix.
- FR-12 When a candidate is blacklisted, delete its profile and import the replacement, so the Network settings list only ever shows profiles that are live candidates.

**Per-app mode**

- FR-13 Create and remove the namespace, veth pair, NAT rule and nftables rules idempotently. Running create twice, or cleanup after a crash, must leave no stray interfaces or rules.
- FR-14 "Launch in VPN" list (default: Chrome with its own profile). Each entry is launched as the logged-in user, not root, inside the namespace.
- FR-15 Kill switch is always on: while the namespace has no live tunnel, it has no route out.

**UI & settings**

- FR-16 **Icon colours.** The tray icon uses three colours only:
  - **White** = running, not connected. This is the state at every startup.
  - **Green** = connected and passing health checks.
  - **Red** = error: no working server after all retries, all sources unreachable, helper service not running, or (on Windows) the VPN driver missing.
  - While connecting or switching, the icon blinks white/green every 0.5 s.
  - Give the white icon a thin dark outline so it stays visible on light panels, and repeat the state in the tooltip in words for colour-blind users.
  - Ship PNG for Linux (22, 24, 32, 48 px), ICO for Windows (16–256 px) and Android vector drawables.
- FR-17 **Autostart** loads the tray icon only and never connects:
  - deb and rpm: `/etc/xdg/autostart/vpntray.desktop`.
  - AppImage: writes `~/.config/autostart/vpntray.desktop` on first run, pointing at the AppImage's current path.
  - Flatpak: requests autostart through the XDG Background portal.
  - Windows installer and portable: `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`. The portable exe writes its own path there on first run, and updates it if the folder moves.
  - Android has no tray: a Quick Settings tile and the app icon replace it, and nothing runs at boot.
  - The "Start at login" toggle in the menu turns it off and on again.
- FR-18 **Settings** live in the tray menu as toggles: mode (per-app / system-wide), Start at login, notifications, and an "Edit settings file" item for the rest (apps list, max profiles, health-check URL and interval, source order). The file is `config.toml`. No settings window in v1.1.
- FR-19 **Log file**, rotated at 1 MB × 3, with an "Open log" menu item. Linux: `~/.local/state/vpntray/vpntray.log`. Windows: `%LOCALAPPDATA%\VPNTray\logs`. Android: an in-app log screen.

## Server list sources

There are not 10 independent VPN Gate equivalents. Only two public sources publish free, ready-to-use OpenVPN configs that can be fetched automatically: VPN Gate and VPNBook. Two open-source loaders were checked ([lovpn](https://github.com/AKotov-dev/lovpn) and [free-vpn-downloader](https://github.com/nemmusu/free-vpn-downloader)), and their code pulls from those two and nothing else. Most other "free VPN" sites make you create a short-lived account behind a captcha, so they can't be automated reliably. The app is therefore built around a **source plugin interface**: each source is one small module, and new ones can be added later without touching the core.

| # | Source | Tier | What it gives | Countries | Notes |
| --- | --- | --- | --- | --- | --- |
| 1 | [VPN Gate API](https://www.vpngate.net/api/iphone/) | A – auto, primary | CSV: IP, speed, ping, **current sessions**, uptime and the full .ovpn per server | Many; changes hourly | Official. Volunteer servers run by a University of Tsukuba research project. Rank on `NumVpnSessions` (current users), not `TotalUsers` (all-time) |
| 2 | VPN Gate via public web proxies | A – auto, fallback | Same CSV, fetched through PHP proxy mirrors | Same | The four mirrors hard-coded in [ovfinder](https://github.com/andy-portmen/ovfinder) (helloapp.site, m.amv.org.mx, proxy.tfdracing.nl, nakabel.com). **Third-party and unofficial**; any may be dead. Use only when #1 is unreachable |
| 3 | Local cache | A – auto, fallback | Last good merged list | Same | Used when every online source fails. Show its age in the menu |
| 4 | [VPNBook](https://www.vpnbook.com/freevpn) | B – scrape | A zip of .ovpn files per server; shared user `vpnbook`, password shown on the page and changed periodically | About 6 | Official provider site, but the password has to be scraped from HTML and may break when the page changes. No speed or user data, so rank by ping only |
| 5 | [FreeOpenVPN.org](https://www.freeopenvpn.org/) | B – scrape | Static .ovpn per country; user `freeopenvpn`, password changes daily | A handful | Login details come from a **third-party** forum post, not verified. Open question: is the password text or an image? If it's an image, drop this source |
| 6 | [vpnobratno.info](https://vpnobratno.info/) | B – scrape | Regularly updated .ovpn list, login `vpn`/`vpn` | Russia only | **Third-party** site; probably re-publishes VPN Gate servers. Optional, off by default |
| 7 | [Proton VPN Free](https://protonvpn.com/free-vpn/linux) | C – manual import | OpenVPN configs downloaded from your free Proton account | Assigned by Proton; you can't choose | Official. The most trustworthy option (no-logs, audited), but it doesn't support picking a country. Offer it as "Trusted fallback" |
| 8 | Your own VPS | C – manual import | Your own .ovpn (or WireGuard later) | Wherever you rent the server | Most reliable and private for a fixed country. Costs a few dollars a month per country |
| 9 | Custom URL | C – user-defined | Any URL returning VPN Gate-format CSV, or a zip of .ovpn files | Any | Settings → Sources → Add. Covers new list sites that appear later |
| 10 | Account sites (tcpvpn.com, vpnjantit.com, opentunnel.net) | D – not automated | Free accounts valid about 5–7 days, created through a web form with a captcha | Many | **Third-party.** Don't scrape them (captcha, terms of use). Users can still import such .ovpn files by hand through source 9 or a local folder |

**Fallback order:** 1 → 2 → 4 → 5 → 6 → 3, then the imported sources 7 to 9. The country list merges every enabled source and removes duplicate IPs. A server's source is shown in the Server submenu as a small tag, for example `VG` or `VB`.

## Ranking and failover

The score is **speed per current user**, with a small penalty for high ping. This fixes the main flaw in the manual method: the browser extension's "users" column is all-time users, not the people connected right now.

```latex
\text{score} = \frac{\text{speed\_mbps}}{\text{sessions} + 1} \times \frac{1}{1 + \text{ping\_ms} / 200}
```

**Filters before scoring:** must have an OpenVPN config, speed of at least 5 Mbps, uptime of at least 30 minutes (servers that just came up often vanish again), and not blacklisted. Servers from sources with no speed data (VPNBook) score `1 / (1 + ping/200)` and rank after VPN Gate servers. Put the thresholds in Settings.

**Failover loop**

1. Pick the top 3 servers by score for the chosen country. These are the candidates. In system-wide mode, create or replace their 3 profiles.
2. Connect to candidate 1 and allow 20 seconds to come up.
3. Run the first health check. If it passes, the state is Connected. Recheck every 20 seconds.
4. On a failed connect, or two failed checks in a row: blacklist that IP for 60 minutes and move to the next candidate.
5. Once all 3 candidates have failed: refetch the list, which skips blacklisted IPs, take a new top 3, and go back to step 2.
6. If 3 refetches in a row produce no working server: set the icon to red, notify "No working servers in \<country> right now", and retry every 5 minutes until the user changes country or disconnects.
7. When the laptop resumes from suspend or the network changes, run a health check right away instead of waiting up to 20 seconds.

The same logic already works as a tested CLI prototype, `vpngate-auto.py` (system-wide mode, VPN Gate only). Gemini can reuse its parser and ranking.

## Architecture

&#91;embedded content: VPN Tray architecture · tray, root helper, two routing modes\]

The tray sends commands over a Unix socket to the root helper. The helper fetches and ranks servers, then runs `openvpn` either inside the `vpntray` namespace (per-app) or through NetworkManager (system-wide). Apps started outside the namespace, such as Firefox, keep using the home connection.

The diagram shows Linux. Windows uses the same tray with the VPNTray Helper service and system-wide routing only. Android has no root helper: Android's own VPN service does the per-app routing.

## Tech stack and platforms

Windows and Android rule out Python + GTK, so the desktop app is written in **Go**: one codebase for Linux and Windows, a single small binary, and no runtime to install. Android is a separate **Kotlin** app that uses the same ranking rules and source list.

| Part | Choice | Notes |
| --- | --- | --- |
| Desktop language | Go (current stable) | One binary per OS/arch; `vpntray` runs the tray, `vpntray daemon` runs the helper |
| Tray icon + menu | [fyne.io/systray](https://github.com/fyne-io/systray) | Windows and Linux. On Linux it uses StatusNotifierItem over D-Bus (Cinnamon, KDE, XFCE; GNOME needs the AppIndicator extension) |
| Notifications | D-Bus notifications (Linux), toast notifications (Windows) |  |
| Linux helper | `vpntrayd` systemd service (root), Unix socket `/run/vpntray.sock`, group `vpntray` | Namespaces, nftables, `openvpn`, `nmcli` |
| Windows helper | "VPNTray Helper" Windows service (LocalSystem), named pipe with an ACL for the installing user | Runs a bundled OpenVPN 2.6 `openvpn.exe` with the ovpn-dco or Wintun driver, which needs admin to install |
| Android | Kotlin + Android `VpnService`; OpenVPN core from [ics-openvpn](https://github.com/schwabe/ics-openvpn) | Per-app routing is built into Android's VPN API (an allowed-apps list). ics-openvpn is GPL, so the source must be published under GPL |
| HTTP | Go `net/http`; Android `HttpURLConnection` | No extra dependencies |

**Platform matrix (v1.1)**

| Build | Arch | Per-app (Chrome only) | System-wide | Autostart | Notes |
| --- | --- | --- | --- | --- | --- |
| .deb | x86\_64, arm64 | Yes | Yes | `/etc/xdg/autostart` | Debian, Ubuntu, LMDE, Mint, Raspberry Pi OS 64-bit. Installs the helper service |
| .rpm | x86\_64, aarch64 | Yes | Yes | `/etc/xdg/autostart` | Fedora, openSUSE. Installs the helper service |
| AppImage | x86\_64, aarch64 | After a one-time "Enable per-app mode" (installs the helper through `pkexec`) | Yes, via NetworkManager | Written on first run | No install needed |
| Flatpak | x86\_64, aarch64 | No: the sandbox can't create namespaces or run a root service | Yes, via NetworkManager over system D-Bus | Background portal | Host needs `network-manager-openvpn` |
| Windows installer .exe | x64, arm64 | No in v1.1 (would need a WFP driver) | Yes | `HKCU\…\Run` | Installs the helper service and OpenVPN driver |
| Windows portable .exe (zip) | x64, arm64 | No | Yes | `HKCU\…\Run` (own path) | Asks for admin (UAC) once, at first connect, to install the driver and helper. A "Remove helper" menu item undoes that |
| Android .apk | arm64-v8a, armeabi-v7a, x86\_64 (one universal APK) | Yes, natively | Yes | Quick Settings tile | Sideloaded; needs "Install unknown apps" |

**Files**

- Linux: binary in `/usr/bin/vpntray`, service `/etc/systemd/system/vpntrayd.service`, settings in `~/.config/vpntray/config.toml`, cache in `~/.cache/vpntray/` (in-use .ovpn files at mode 600), state in `~/.local/state/vpntray/`. The `Chrome (VPN)` launcher goes in `~/.local/share/applications/`.
- Windows: the installer puts the app in `%ProgramFiles%\VPNTray`; portable runs from any folder. Settings in `%APPDATA%\VPNTray\config.toml`, cache and logs in `%LOCALAPPDATA%\VPNTray`.
- Uninstalling on any platform removes every `vpntray-*` profile, the namespace or service, and the autostart entry.

## Release v1.1 — built by GitHub Actions

Pushing the tag `v1.1.0` runs `.github/workflows/release.yml`, which builds everything and publishes one GitHub Release named **v1.1**, with every file below plus `SHA256SUMS.txt`. The workflow can also be started by hand (`workflow_dispatch`) for a test build without publishing. One `VERSION` file (`1.1.0`) feeds every build.

Each architecture builds on its own native GitHub runner (`ubuntu-24.04`, `ubuntu-24.04-arm`, `windows-latest`, `windows-11-arm`), so nothing has to be cross-compiled.

| File | Built with | Runner |
| --- | --- | --- |
| `vpntray_1.1.0_amd64.deb`, `vpntray_1.1.0_arm64.deb` | GoReleaser + nFPM | ubuntu-24.04 / -arm |
| `vpntray-1.1.0.x86_64.rpm`, `vpntray-1.1.0.aarch64.rpm` | GoReleaser + nFPM | ubuntu-24.04 / -arm |
| `VPNTray-1.1.0-x86_64.AppImage`, `VPNTray-1.1.0-aarch64.AppImage` | `appimagetool` | ubuntu-24.04 / -arm |
| `VPNTray-1.1.0-x86_64.flatpak`, `VPNTray-1.1.0-aarch64.flatpak` | `flatpak-builder` + `flatpak build-bundle` | ubuntu-24.04 / -arm |
| `VPNTray-Setup-1.1.0-x64.exe`, `VPNTray-Setup-1.1.0-arm64.exe` | Inno Setup, bundling OpenVPN and its driver | windows-latest / windows-11-arm |
| `VPNTray-1.1.0-portable-x64.zip`, `VPNTray-1.1.0-portable-arm64.zip` | Go build with `-ldflags -H=windowsgui` (no console window), zipped with the OpenVPN binaries | windows-latest / windows-11-arm |
| `VPNTray-1.1.0.apk` | Gradle `assembleRelease`, universal APK | ubuntu-24.04 |

**Secrets the workflow needs:** `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS` and `ANDROID_KEY_PASSWORD` to sign the APK. `GITHUB_TOKEN` is built in. Windows files are unsigned in v1.1, so Windows SmartScreen will warn on first run. Adding a code-signing certificate later only needs one more secret.

**Release checks in the workflow:** install the deb and rpm in clean containers, run `vpntray --version` for every Linux and Windows binary, run the unit tests for ranking and parsing, and fail the release if any file is missing.

## Security and privacy

Free public servers change your country; they don't make you private. The app must say so plainly and must not make things worse.

- **Who sees what.** Each server is run by an unknown volunteer who can see your traffic metadata. VPN Gate says it keeps connection logs for at least three months. Show a one-time notice on first run: use it for geo-access, keep to HTTPS sites, and don't do banking through it.
- **Weak configs.** VPN Gate configs use AES-128-CBC with SHA1 and don't check that the server is the real one. Accept them as they are, but never weaken anything further. Show a lock icon in the menu only for servers that use modern ciphers.
- **No leaks in per-app mode.** nftables in the namespace allows only the VPN server's IP and port on the veth link, plus everything on `tun0`. Block IPv6 in the namespace (`net.ipv6.conf.all.disable_ipv6=1`) unless the tunnel carries IPv6. Chrome (VPN) launches with `--force-webrtc-ip-handling-policy=disable_non_proxied_udp`.
- **Least privilege.** The tray runs as the user. The root helper accepts only a fixed set of commands: `connect <server-id>`, `next`, `disconnect`, `status`, `launch <app-id>`, `cleanup`. It never accepts a free-form command or file path. Apps listed in Settings launch as the user.
- **Untrusted input.** Treat every downloaded .ovpn as hostile. Before use, strip `up`, `down`, `script-security`, `plugin`, `route-up`, `ipchange`, `tls-verify` and any other line that can run a script, and reject configs containing them if stripping fails. Validate that the `remote` matches the listed IP.
- **Secrets.** VPNBook/FreeOpenVPN passwords live only in memory and in the `auth-user-pass` file in use (mode 600), which is deleted on disconnect.
- **No telemetry.** The app talks only to the enabled sources, the health-check URL, and the geo-IP URL if that's turned on.

## Acceptance criteria

- [ ] Fresh Debian 13 / LMDE 7: install the .deb, log out and back in, and the tray icon appears with no other setup.
- [ ] Tray, Choose country, Japan: connected in under 30 seconds when servers are up.
- [ ] Per-app: `curl ifconfig.me` in a normal terminal shows the home IP. In Chrome (VPN), an IP-check site shows the VPN server's IP and country. Firefox shows the home IP.
- [ ] Per-app kill switch: run `sudo ip netns exec vpntray ip link set tun0 down`, and Chrome (VPN) loads nothing within 5 seconds and never shows the home IP.
- [ ] DNS leak test inside Chrome (VPN) shows no home-ISP resolvers.
- [ ] Failover: block the current server's IP with nftables on the host, and the app moves to candidate 2 within 60 seconds and sends one notification.
- [ ] All 3 candidates blocked: the app refetches and connects to a new server without user action.
- [ ] Country list paginates 10 per page, Next/Previous work, and the last 3 countries are pinned.
- [ ] System-wide mode: after 10 failovers, `nmcli connection show` lists no more than 3 `vpntray-*` profiles, and no non-`vpntray` profile was changed.
- [ ] Next server, picking a specific server, and Disconnect each respond in under 2 seconds.
- [ ] With vpngate.net blocked in `/etc/hosts`, the list still loads from a fallback source or the cache, and the menu shows which.
- [ ] Quit, or kill -9 then restart: no leftover namespace, veth, nftables table or stray profiles.
- [ ] Suspend and resume: the connection is health-checked within 5 seconds of resume and recovers.
- [ ] Idle RAM under 60 MB for tray and helper combined; CPU near 0% between checks.

**Startup, icon and platforms**

- [ ] Reboot on Debian/LMDE, Fedora and Windows 11: the icon appears in the tray by itself after login, white, and nothing connects.
- [ ] Icon colours: white when idle, blinks while connecting, green when connected, red with every source blocked or the helper stopped. The tooltip names each state.
- [ ] The white icon is visible on both a light and a dark panel.
- [ ] Turning off "Start at login" stops the icon appearing after the next reboot; turning it on brings it back.
- [ ] Pushing tag `v1.1.0` produces a GitHub Release named v1.1 with all 13 files plus `SHA256SUMS.txt`, and no manual steps.
- [ ] Each file installs or runs on its target: deb (Debian 13 x86\_64 and Raspberry Pi OS 64-bit), rpm (Fedora x86\_64 and aarch64), AppImage and Flatpak (both arches), Windows installer and portable (Windows 11 x64 and arm64), APK (one arm64 phone).
- [ ] Windows: connecting changes the public IP to the chosen country. The portable build asks for admin only once.
- [ ] Android: with only Chrome in the allowed-apps list, Chrome shows the VPN IP and every other app shows the home IP. The Quick Settings tile connects and disconnects.

## Build order for Gemini

Build in this order, and get each stage working and tested before starting the next. Attach `vpngate-auto.py` as the reference for parsing and ranking.

1. **Go core:** sources, parser and ranking, ported from `vpngate-auto.py`, with unit tests against a saved CSV.
2. **Linux system-wide:** connect, disconnect and cleanup through `nmcli`, the 3-profile cap and the failover loop, plus a CLI to drive it.
3. **Tray:** fyne.io/systray menu, country pagination, Server submenu, white/green/red icons with blinking, notifications, Start at login.
4. **Linux per-app helper:** namespace, veth, NAT and nftables kill switch, `openvpn` inside the namespace, launching Chrome as the user. Then make per-app the Linux default.
5. **Windows:** VPNTray Helper service, bundled OpenVPN and driver, named-pipe control, autostart, installer and portable modes.
6. **Fallback sources:** VPN Gate mirrors, VPNBook, FreeOpenVPN and custom URL, plus the cache.
7. **Release workflow:** deb, rpm, AppImage, Flatpak and Windows builds on native runners. Test with `workflow_dispatch` before tagging.
8. **Android app:** Kotlin, `VpnService` with an allowed-apps list, ics-openvpn core, Quick Settings tile. Add it to the workflow.
9. Run the acceptance criteria, then push tag `v1.1.0`.

**Out of scope for v1.1:** macOS and iOS, WireGuard, per-app mode on Windows, Google Play / Microsoft Store publishing, code-signing certificates, paid-provider accounts, automated sign-up on account-based sites, a server map, and speed tests run by the app itself.

**Open questions**

- Is FreeOpenVPN.org's daily password plain text or an image? This decides whether source 5 can ship.
- "arm" is taken to mean 64-bit ARM (arm64/aarch64). Add 32-bit armhf builds for older Raspberry Pi OS as well?
- The Android app uses ics-openvpn, which is GPL. The repository must be public under the GPL if the APK is shared with anyone. Is that OK?
- Windows builds are unsigned in v1.1, so SmartScreen will warn on first run. Acceptable, or buy a code-signing certificate?
- Should per-app mode also offer a local SOCKS5 proxy into the namespace (for example `127.0.0.1:1080`), so you could switch single Firefox containers onto the VPN? Nice to have.
