#!/bin/sh
set -e

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl stop myovpnd.service || true
    systemctl disable myovpnd.service || true
fi

if [ -x /usr/bin/myovpn ]; then
    /usr/bin/myovpn cleanup || true
fi

rm -rf /var/lib/myovpn /var/cache/myovpn /run/myovpn.sock
