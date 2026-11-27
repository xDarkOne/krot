#!/bin/bash
set -euo pipefail
# Routes for an additional client TUN using Krot's split-routing marks.
action=$1
interface=$2
network=$3
priority=$4
if [ "$action" = down ]; then
  ip -4 rule del from "$network" fwmark 1 table main priority "$priority" 2>/dev/null || true
  ip -4 rule del from "$network" table 200 priority "$((priority + 1))" 2>/dev/null || true
  ip -4 route del "$network" dev "$interface" table 200 2>/dev/null || true
  exit 0
fi
for _ in $(seq 1 30); do
  if ip -4 addr show dev "$interface" scope global 2>/dev/null | grep 'inet ' >/dev/null; then break; fi
  sleep 1
done
bash "$0" down "$interface" "$network" "$priority"
ip -4 route replace "$network" dev "$interface" table 200
ip -4 rule add from "$network" fwmark 1 table main priority "$priority"
ip -4 rule add from "$network" table 200 priority "$((priority + 1))"
