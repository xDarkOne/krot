# hysteria2 — быстрый UDP-вход подписки (sing-box)

**Роли:** entry, solo. **Зависит от:** xray, nginx-tls. **Код:** [agent/hysteria2/](../../agent/hysteria2/)

Модуль даёт быстрый вход для клиентов по UDP/QUIC. Он полезен на плохих и мобильных сетях и раздаётся подпиской. Трафик принимает sing-box и передаёт в xray, поэтому маршрутизация общая с xray.

## Как устроено
- Ставится пакет `sing-box` версии `VK_SINGBOX_VERSION` из GitHub releases SagerNet (`.deb` для amd64/arm64). sha256 сверяется с `VK_SINGBOX_SHA256_AMD64`/`VK_SINGBOX_SHA256_ARM64`. Пакет ставится `dpkg -i --force-confold`, копия сохраняется в `/var/lib/vpnkit/pkgcache/`.
- Проверяется наличие `/etc/vpnkit/xray.env` (модуль xray) и сертификата `/etc/letsencrypt/live/$VK_TUNNEL_NAME/fullchain.pem` (модуль nginx-tls).
- Проверяется UDP-порт `$VK_HY2_PORT`: если его слушает не `sing-box`, установка останавливается.
- Файлы:
  - `/etc/sing-box/config.json` — пишет `vpnkit-gen.py`;
  - `/etc/systemd/system/sing-box.service.d/10-vpnkit-single-config.conf` — заставляет службу запускать только `config.json`: `/usr/bin/sing-box -D /var/lib/sing-box -c /etc/sing-box/config.json run`;
  - `/etc/vpnkit/hy2.env` — параметры для генератора: `TUNNEL_NAME`, `HY2_PORT`;
  - `/etc/nftables.d/46-vpnkit-hy2.nft` — правило `udp dport $VK_HY2_PORT accept` в таблице `inet vpnkit_fw`.
- После записи правила выполняется `nft -c -f /etc/nftables.conf` и `systemctl reload nftables`.
- Служба `sing-box.service` включается. `vpnkit-gen.py build` может сам перезапустить sing-box при изменении конфига; установка дополнительно перезапускает службу, если изменилась версия пакета или drop-in каталог, либо служба не активна, и генератор не сообщил о перезапуске.
- Клиенты берутся из `/etc/vpnkit/users.json` (секреты модуля по манифесту).
- В каскаде модуль — entry: принимает клиентский трафик и сдаёт его в xray. На одном сервере — solo: установка и проверка те же, xray и nginx-tls должны быть на том же узле.

## Параметры

Переменные `VK_*` готовит пульт. Те, что имеет смысл менять, задаются в inventory как `modules.<модуль>.<имя>` (имя — в нижнем регистре, без `VK_`).
| Переменная | Умолчание | Что значит |
|---|---|---|
| `hy2_port` (`VK_HY2_PORT`) | `443` | UDP-порт hysteria2 |
| `VK_TUNNEL_NAME` | — | имя каталога сертификата в `/etc/letsencrypt/live/` |
| `VK_SINGBOX_VERSION` | — | пин версии sing-box |
| `VK_SINGBOX_SHA256_AMD64` | — | sha256 `.deb` sing-box для amd64 |
| `VK_SINGBOX_SHA256_ARM64` | — | sha256 `.deb` sing-box для arm64 |

## Проверка
`verify.sh` выполняет:
- `sing-box check -c /etc/sing-box/config.json`;
- ожидание 2 секунды и проверку, что `sing-box` активен;
- проверку, что UDP-порт слушает именно `sing-box`: `ss -Hlunp "sport = :$VK_HY2_PORT"`;
- проверку, что в журнале sing-box за последнюю минуту нет строк `error`/`fatal`;
- проверку правила `udp dport $VK_HY2_PORT accept` в `inet vpnkit_fw input`.

Полезные команды:
```sh
sing-box check -c /etc/sing-box/config.json
systemctl status sing-box
ss -Hlunp 'sport = :443'
journalctl -u sing-box --since -1min -o cat --no-pager | grep -ci 'error\|fatal'
nft list chain inet vpnkit_fw input
```

## Ловушки
- [P-20](../60-pitfalls.md): после обновления сертификата sing-box может держать старый в памяти; deploy-hook certbot должен перезапускать `sing-box`.

## Откат
`rollback.sh`:
- останавливает и отключает `sing-box`, если пакет был установлен этим прогоном (`created.list`);
- восстанавливает снапшот `/etc/sing-box/config.json`;
- выполняет `systemctl daemon-reload`;
- перезагружает nftables;
- запускает `vpnkit-gen.py build`, если генератор есть;
- перезапускает `sing-box`, если служба включена.

Пакет `sing-box` после отката остаётся установленным.
