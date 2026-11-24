#!/usr/bin/env bash
# sahand-mirror-companion installer
#   from your hub (recommended; the hub fills in its own address):
#       bash <(curl -Ls http://HUB-IP:PORT/companion)
#   from GitHub, pointing at a hub:
#       bash <(curl -Ls https://raw.githubusercontent.com/sahand363/sahand-mirror-companion/main/install.sh) --hub http://HUB-IP:PORT
set -euo pipefail

REPO="sahand363/sahand-mirror-companion"
BIN=/usr/local/bin/sahand-mirror-companion
LINK=/usr/local/bin/smc
CONF=/etc/sahand-mirror-companion/config.json
UNIT=/etc/systemd/system/sahand-mirror-companion.service
HUB="${SMC_HUB:-${MIRROR_URL:-}}"
KEY="${SMC_KEY:-${__SM_KEY:-}}"
PORT="${SMC_PORT:-}"
VERSION="${SMC_VERSION:-}"

tty_ok() { [ -e /dev/tty ] && { : </dev/tty; } 2>/dev/null; }
red() { echo -e "\033[31m$*\033[0m"; }
green() { echo -e "\033[32m$*\033[0m"; }
die() { red "error: $*"; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --hub) HUB="${2:-}"; shift 2 ;;
    --key) KEY="${2:-}"; shift 2 ;;
    --port) PORT="${2:-}"; shift 2 ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    *) die "unknown option $1" ;;
  esac
done

[ "$(id -u)" = 0 ] || die "run as root"
command -v systemctl >/dev/null || die "systemd is required"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7*|armv8l|armhf) ARCH=armv7 ;;
  *) die "unsupported architecture $(uname -m)" ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Inside the hub's install wrapper, curl rewrites these GitHub URLs to the hub.
BASE="https://github.com/$REPO"
if [ -n "$VERSION" ]; then URL="$BASE/releases/download/$VERSION/sahand-mirror-companion-linux-$ARCH.tar.gz"
else URL="$BASE/releases/latest/download/sahand-mirror-companion-linux-$ARCH.tar.gz"; fi
echo "downloading sahand-mirror-companion ($ARCH)${MIRROR_URL:+ from hub $MIRROR_URL}..."
curl -fL --retry 3 -o "$TMP/smc.tar.gz" "$URL" || die "download failed ($URL). Is $REPO mirrored on the hub?"
if curl -fsL -o "$TMP/smc.tar.gz.sha256" "$URL.sha256" 2>/dev/null; then
  (cd "$TMP" && echo "$(awk '{print $1}' smc.tar.gz.sha256)  smc.tar.gz" | sha256sum -c - >/dev/null) || die "checksum mismatch"
fi
tar -xzf "$TMP/smc.tar.gz" -C "$TMP" sahand-mirror-companion

FRESH=0
[ -f "$CONF" ] || FRESH=1
if [ "$FRESH" = 1 ] && [ -z "$HUB" ]; then
  tty_ok && read -r -p "sahand-mirror hub address (e.g. http://10.0.0.5:8686): " HUB </dev/tty || true
  [ -n "$HUB" ] || die "hub address is required (--hub http://IP:PORT)"
fi
if [ "$FRESH" = 1 ] && [ -z "$PORT" ]; then
  if tty_ok && [ -z "${SMC_NO_MENU:-}" ]; then read -r -p "panel port [8687]: " PORT </dev/tty || true; fi
fi
PORT="${PORT:-0}"
[[ "$PORT" =~ ^[0-9]+$ ]] && [ "$PORT" -le 65535 ] || die "invalid port $PORT"
[ "$FRESH" = 1 ] && [ "$PORT" = 0 ] && PORT=8687

systemctl stop sahand-mirror-companion 2>/dev/null || true
install -m 0755 "$TMP/sahand-mirror-companion" "$BIN"
ln -sf "$BIN" "$LINK"

INIT_ARGS=()
[ -n "$HUB" ] && INIT_ARGS+=(--hub "$HUB")
[ -n "$KEY" ] && INIT_ARGS+=(--key "$KEY")
[ "$PORT" != 0 ] && INIT_ARGS+=(--port "$PORT")
INIT_OUT="$("$BIN" init ${INIT_ARGS[@]+"${INIT_ARGS[@]}"})"

cat >"$UNIT" <<UNIT_EOF
[Unit]
Description=sahand-mirror-companion (national-internet simulator and hub app catalog)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=$BIN serve
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT_EOF
systemctl daemon-reload
systemctl enable sahand-mirror-companion >/dev/null 2>&1 || true
systemctl restart sahand-mirror-companion || die "service failed to start (journalctl -u sahand-mirror-companion)"
command -v iptables >/dev/null || red "note: iptables is not installed; the national-internet simulator needs it (apt install iptables)"

echo
green "sahand-mirror-companion $("$BIN" version) installed."
IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
P="$(grep -o '"listen": *"[^"]*"' "$CONF" | sed 's/.*:\([0-9]*\)"$/\1/')"
echo "panel:    http://${IP:-SERVER-IP}:${P:-8687}"
echo "hub:      $(grep -o '"hub": *"[^"]*"' "$CONF" | sed 's/.*"\(http[^"]*\)"/\1/')"
if echo "$INIT_OUT" | grep -q 'ui password'; then
  echo "user:     $(echo "$INIT_OUT" | awk -F': ' '/ui user/{print $2}')"
  echo "password: $(echo "$INIT_OUT" | awk -F': ' '/ui password/{print $2}')"
  echo "(save it now; change it any time from the menu: smc)"
fi
echo
if [ -z "${SMC_NO_MENU:-}" ] && [ -t 1 ] && tty_ok; then
  exec "$BIN" menu </dev/tty
fi
