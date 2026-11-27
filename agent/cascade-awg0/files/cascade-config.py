#!/usr/bin/python3
"""Build independent cascade services without touching the client access configs."""

import json
import os
import sys


def env(name):
    return os.environ["VK_" + name]


def xray():
    name = env("REALITY_NAME")
    short_id = env("REALITY_SHORT_ID")
    uuid = env("REALITY_UUID")
    flow = "xtls-rprx-vision"

    def reality(settings):
        return {"network": "raw", "security": "reality", "realitySettings": settings}

    if env("SIDE") == "server":
        return {
            "log": {"loglevel": "warning", "access": "none"},
            "inbounds": [{
                "listen": "0.0.0.0", "port": int(env("REALITY_PORT")), "protocol": "vless",
                "settings": {"decryption": "none", "clients": [{"id": uuid, "flow": flow}]},
                "streamSettings": reality({
                    "target": f"{name}:443", "serverNames": [name],
                    "privateKey": env("REALITY_PRIVATE"), "shortIds": [short_id],
                }),
                "sniffing": {"enabled": True, "destOverride": ["http", "tls", "quic"], "routeOnly": True},
            }],
            "outbounds": [
                {"tag": "internet", "protocol": "freedom", "streamSettings": {"sockopt": {"domainStrategy": "UseIPv4"}}},
                {"tag": "blocked", "protocol": "blackhole"},
            ],
            "routing": {"domainStrategy": "IPOnDemand", "rules": [
                {"protocol": ["bittorrent"], "outboundTag": "blocked"},
                {"ip": ["0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
                        "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16",
                        "224.0.0.0/4", "::/128", "::1/128", "fc00::/7", "fe80::/10"],
                 "outboundTag": "blocked"},
            ]},
        }
    return {
        "log": {"loglevel": "warning", "access": "none"},
        "inbounds": [{
            "listen": "127.0.0.1", "port": 19453, "protocol": "socks",
            "settings": {"auth": "noauth", "udp": True, "ip": "127.0.0.1"},
        }],
        "outbounds": [{
            "protocol": "vless",
            "settings": {"vnext": [{"address": env("EXIT_IP"), "port": int(env("REALITY_PORT")),
                                    "users": [{"id": uuid, "encryption": "none", "flow": flow}]}]},
            "streamSettings": reality({"serverName": name, "fingerprint": "chrome",
                                       "password": env("REALITY_PUBLIC"), "shortId": short_id}),
        }],
    }


def tun():
    return {
        "log": {"level": "warn"},
        "inbounds": [{
            "type": "tun", "tag": "tun-in", "interface_name": "sbtun",
            "address": ["172.31.31.1/30"], "mtu": 1380, "auto_route": False,
            "strict_route": False, "stack": "gvisor",
        }],
        "outbounds": [{"type": "socks", "tag": "to-exit", "server": "127.0.0.1",
                       "server_port": 19453, "version": "5"}],
        "route": {"final": "to-exit"},
    }


def watch():
    return {
        "peer": env("PEER_PUBLIC"), "tunnel_ip": env("PEER_TUN"),
        "endpoint": env("ENDPOINT"), "endpoint_v6": os.getenv("VK_ENDPOINT_V6", ""),
        "exit_host": env("EXIT_IP"), "exit_ssh_port": int(env("EXIT_SSH_PORT")),
        "backup": bool(os.getenv("VK_REALITY_NAME")),
        "reality_port": int(os.getenv("VK_REALITY_PORT", "0")),
        "telegram_prefixes": env("TG_PREFIXES").split(),
    }


json.dump({"xray": xray, "tun": tun, "watch": watch}[sys.argv[1]](), sys.stdout, indent=2)
print()
