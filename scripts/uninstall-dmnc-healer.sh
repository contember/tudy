#!/usr/bin/env bash
# Remove the dmnc healer LaunchDaemon installed by install-dmnc-healer.sh.
set -euo pipefail

LABEL="dev.tudy.dmnc-healer"
PLIST="/Library/LaunchDaemons/${LABEL}.plist"

if [[ "$EUID" -ne 0 ]]; then
    exec sudo -- "$0" "$@"
fi

launchctl bootout system/"$LABEL" 2>/dev/null || true
rm -f "$PLIST"
echo "Uninstalled: $LABEL"
