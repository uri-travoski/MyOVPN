#!/bin/sh
set -e

if ! getent group myovpn >/dev/null 2>&1; then
    groupadd --system myovpn || true
fi

# Add regular desktop users (UID 1000..59999) so tray connects cleanly after next login
for u in $(awk -F: '$3 >= 1000 && $3 < 60000 {print $1}' /etc/passwd); do
    usermod -aG myovpn "$u" 2>/dev/null || true
done

mkdir -p /var/lib/myovpn /var/cache/myovpn

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl daemon-reload || true
    systemctl enable myovpnd.service || true
    systemctl restart myovpnd.service || true
fi

if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
fi
