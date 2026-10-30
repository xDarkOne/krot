#!/usr/bin/env python3
"""Чистит ru_domains от адресов общих облаков и CDN.

dnsmasq кладёт в набор адреса всех доменов российских зон. Если российский сайт живёт на
инфраструктуре Google, Cloudflare или AWS, туда попадает ОБЩИЙ адрес, который обслуживает заодно
и сам Google. Тогда часть запросов к Google уходит напрямую с адреса хостинга, и сервис считает,
что пользователь в России, хотя основной трафик идёт через заграничный узел. Отличить российский
сервис от чужого на общем хостинге по адресу нельзя, поэтому такие адреса убираем: пусть идут через туннель.
"""
import ipaddress
import re
import subprocess
import sys

SET = ["inet", "vpnkit_split", "ru_domains"]
CLOUDS = [ipaddress.ip_network(n) for n in """
142.250.0.0/15 142.251.0.0/16 172.217.0.0/16 216.58.192.0/19 64.233.160.0/19 74.125.0.0/16
192.178.0.0/15 35.184.0.0/13 104.16.0.0/12 172.64.0.0/13 162.158.0.0/15 198.41.128.0/17
151.101.0.0/16 199.232.0.0/16 23.32.0.0/11 23.192.0.0/11 13.32.0.0/12 13.224.0.0/14
52.84.0.0/15 54.192.0.0/16""".split()]

p = subprocess.run(["nft", "list", "set"] + SET, capture_output=True, text=True)
if p.returncode:
    sys.exit(0)  # набора нет — делать нечего
removed = 0
for ip in sorted(set(re.findall(r"\d+\.\d+\.\d+\.\d+", p.stdout))):
    a = ipaddress.ip_address(ip)
    if any(a in n for n in CLOUDS):
        if subprocess.run(["nft", "delete", "element"] + SET + ["{ %s }" % ip], capture_output=True).returncode == 0:
            removed += 1
if removed:
    subprocess.run(["logger", "-t", "vpnkit-ru-prune", f"убрано адресов общих облаков: {removed}"])
print(f"убрано: {removed}")
