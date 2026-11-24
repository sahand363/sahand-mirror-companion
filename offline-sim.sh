#!/bin/bash
# offline-sim.sh - simulate an international internet shutdown on a DESTINATION server.
# All outgoing traffic is blocked except loopback, the current SSH session (established
# connections) and the hub. Restores itself automatically after N minutes.
#
#   bash offline-sim.sh [minutes]          start (default 20, minimum 1)
#   ALLOW_DNS=1 bash offline-sim.sh 30     keep DNS open (closer to a real national shutdown)
#   HUB=1.2.3.4 bash offline-sim.sh        another hub IP
#   bash offline-sim.sh off                end now and reconnect to the internet
# Run as root.
set -u
HUB=${HUB:-94.184.6.138}
ALLOW_DNS=${ALLOW_DNS:-0}

restore() {
  systemctl stop offline-restore.timer offline-restore.service 2>/dev/null
  for T in iptables ip6tables; do
    while $T -D OUTPUT -j OFFSIM 2>/dev/null; do :; done
    $T -F OFFSIM 2>/dev/null; $T -X OFFSIM 2>/dev/null
  done
  echo "Offline mode is off. Internet is back."
}

if [ "${1:-}" = off ]; then restore; exit 0; fi
[ "$(id -u)" = 0 ] || { echo "Run as root."; exit 1; }

MIN=${1:-20}
case "$MIN" in ''|*[!0-9]*) echo "Minutes must be a number (or 'off')."; exit 1;; esac
[ "$MIN" -lt 1 ] && MIN=1

restore >/dev/null   # clean any previous run

# automatic restore after MIN minutes, even if SSH drops
systemctl reset-failed offline-restore.service 2>/dev/null
systemd-run --on-active="${MIN}m" --unit=offline-restore /bin/sh -c \
  'for T in iptables ip6tables; do while $T -D OUTPUT -j OFFSIM 2>/dev/null; do :; done; $T -F OFFSIM; $T -X OFFSIM; done' \
  || { echo "systemd-run failed; not blocking without an automatic restore."; exit 1; }

for T in iptables ip6tables; do
  $T -N OFFSIM
  $T -A OFFSIM -o lo -j ACCEPT
  $T -A OFFSIM -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  if [ "$ALLOW_DNS" = 1 ]; then
    $T -A OFFSIM -p udp --dport 53 -j ACCEPT
    $T -A OFFSIM -p tcp --dport 53 -j ACCEPT
  fi
done
iptables -A OFFSIM -d "$HUB" -j ACCEPT
for T in iptables ip6tables; do
  $T -A OFFSIM -p tcp -j REJECT --reject-with tcp-reset
  $T -A OFFSIM -j REJECT
  $T -I OUTPUT 1 -j OFFSIM
done

echo "Offline mode is on. Only the hub ($HUB) is reachable."
echo "Automatic restore in $MIN minute(s). End now: bash offline-sim.sh off"
echo "Check:  curl -m5 -sI https://github.com   (must fail)"
