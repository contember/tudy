#!/usr/bin/env bash
# Install a launchd daemon that periodically checks the
# docker-mac-net-connect (dmnc) tunnel and kills the dmnc process when
# the tunnel is broken — launchd's KeepAlive then restarts dmnc, which
# re-injects its WireGuard peer into Docker Desktop's VM.
#
# Trigger: every 60s, probe dmnc's tunnel endpoint (10.33.33.2:22).
#   - Healthy tunnel: "Connection refused" — no action.
#   - Broken tunnel: timeout/no-route — pkill dmnc and let launchd respawn it.
#
# This catches the common failure mode where Docker Desktop is
# restarted/upgraded after dmnc was started: the VM-side peer disappears
# but the host process keeps running and the tunnel silently black-holes
# traffic.
set -euo pipefail

LABEL="dev.tudy.dmnc-healer"
PLIST="/Library/LaunchDaemons/${LABEL}.plist"
LOG_PATH="/var/log/tudy-dmnc-healer.log"

if [[ "$EUID" -ne 0 ]]; then
    echo "This script needs to install a LaunchDaemon under /Library/LaunchDaemons."
    echo "Re-running with sudo..."
    exec sudo -- "$0" "$@"
fi

cat > "$PLIST" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>dev.tudy.dmnc-healer</string>
    <key>RunAtLoad</key>
    <true/>
    <key>StartInterval</key>
    <integer>60</integer>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/sh</string>
        <string>-c</string>
        <string>/usr/bin/nc -vG 1 -z 10.33.33.2 22 2&gt;&amp;1 | /usr/bin/grep -qi 'refused' &amp;&amp; exit 0; /usr/bin/pgrep -x docker-mac-net-connect &gt;/dev/null &amp;&amp; /usr/bin/pkill -x docker-mac-net-connect &amp;&amp; echo "$(date -u +%FT%TZ) dmnc tunnel broken; killed to trigger launchd respawn"</string>
    </array>
    <key>StandardOutPath</key>
    <string>/var/log/tudy-dmnc-healer.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/tudy-dmnc-healer.log</string>
</dict>
</plist>
EOF

chmod 644 "$PLIST"
chown root:wheel "$PLIST"

# Reload if previously loaded (idempotent install).
launchctl bootout system/"$LABEL" 2>/dev/null || true
launchctl bootstrap system "$PLIST"
launchctl enable system/"$LABEL"

echo
echo "Installed: $LABEL"
echo "Plist:     $PLIST"
echo "Logs:      $LOG_PATH"
echo "Interval:  every 60s"
echo
echo "Uninstall: sudo scripts/uninstall-dmnc-healer.sh"
