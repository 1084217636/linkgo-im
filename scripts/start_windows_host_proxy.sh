#!/usr/bin/env bash
# Local fallback for WSL mirrored networking that does not forward localhost.
set -euo pipefail
cd "$(dirname "$0")/.."

if ! command -v powershell.exe >/dev/null || ! command -v curl.exe >/dev/null; then
  printf 'Windows interop is unavailable; cannot start the host browser bridge.\n' >&2
  exit 1
fi

if curl.exe --noproxy '*' -4 -k -sS -o NUL -w '%{http_code}' \
    --connect-timeout 2 --max-time 4 https://localhost:8443/community/ 2>/dev/null | rg -q '^200$'; then
  printf 'Windows browser bridge already responds on localhost:8443.\n'
  exit 0
fi

win_temp=$(powershell.exe -NoProfile -Command \
  '[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $env:TEMP' | tr -d '\r')
win_temp_linux=$(wslpath -u "$win_temp")
win_script_linux="$win_temp_linux/LinkGo-Win-Proxy.py"
cp scripts/dev_windows_wsl_proxy.py "$win_script_linux"
win_script=$(wslpath -w "$win_script_linux")
bridge_path="$PWD/scripts/dev_wsl_stdio_bridge.py"

# These paths come from the local installation, not a network request.
powershell.exe -NoProfile -Command \
  "\$args = @('$win_script', '--distro', '$WSL_DISTRO_NAME', '--bridge-path', '$bridge_path', '--target-port', '8444', '--listen-port', '8443'); Start-Process -FilePath (Get-Command python.exe).Source -ArgumentList \$args -WindowStyle Hidden" >/dev/null

for i in {1..20}; do
  if curl.exe --noproxy '*' -4 -k -sS -o NUL -w '%{http_code}' \
      --connect-timeout 2 --max-time 4 https://localhost:8443/community/ 2>/dev/null | rg -q '^200$'; then
    printf 'Windows browser entry is ready: https://localhost:8443/community/\n'
    exit 0
  fi
  sleep 1
done
printf 'Windows bridge did not become ready; check port 8443 and Windows Python.\n' >&2
exit 1
