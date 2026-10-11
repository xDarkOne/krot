# xray — входы подписки VLESS поверх HTTPS и VLESS Encryption

**Роли:** entry (каскад), solo.
**Зависит от:** `base-host`, `nginx-tls`; для entry — `split-routing`, `cascade-awg0`.
**Код:** [agent/xray/](../../agent/xray/)

Модуль даёт клиентам подписочные входы, похожие на обычный HTTPS-трафик: XHTTP за nginx и VLESS Encryption (PQ). На entry трафик зарубежных сайтов уходит через каскад, российские — напрямую; на solo всё идёт напрямую.

## Как устроено

Устанавливается:

- бинарник `/usr/local/bin/xray` по пину версии и sha256 архива;
- geo-файлы `/usr/local/share/xray/geosite.dat`, `/usr/local/share/xray/geoip.dat`;
- генератор конфигов `/usr/local/sbin/vpnkit-gen.py`;
- реестр клиентов `/etc/vpnkit/users.json` (создаётся один раз, ключ `vlessenc` при пересборке не меняется);
- параметры модуля `/etc/vpnkit/xray.env`;
- конфиг `/usr/local/etc/xray/config.json` — пишет `vpnkit-gen.py`;
- nginx-конфиг `/etc/nginx/vpnkit.d/<имя>-xhttp-xray.conf`;
- nftables-правило `/etc/nftables.d/45-vpnkit-xray.nft`;
- службы `xray.service`, `vpnkit-gen.service`, таймер `vpnkit-gen.timer`.

Порты:

- `{xhttp_port}` — наружный HTTPS-порт nginx, XHTTP за TLS;
- `{xh_port}` — внутренний порт XHTTP на `127.0.0.1`, куда nginx проксирует путь `{xh_path}`;
- `{pq_port}` — наружный TCP-порт VLESS Encryption;
- `10801` — петлевой SOCKS на `127.0.0.1` для sing-box.

Правило nftables открывает наружный порт PQ:

```nft
tcp dport {pq_port} accept
```

В каскаде (`VK_CASCADE=1`) установка требует список российских сетей `/var/lib/vpnkit/ru-nets.txt` и ip-rule `fwmark 0x3 lookup 200` от `cascade-awg0`. В solo (`VK_CASCADE=0`) эти проверки не нужны.

Служба `xray.service` запускается от `nobody` с `CAP_NET_ADMIN` и `CAP_NET_BIND_SERVICE`. `vpnkit-gen.timer` раз в сутки пересобирает конфиги. Перезапуск `xray` при повторной установке происходит только при смене бинарника или юнита; изменение конфига генератор обрабатывает сам.

## Параметры

Переменные `VK_*` готовит пульт. Те, что имеет смысл менять, задаются в inventory как `modules.<модуль>.<имя>` (имя — в нижнем регистре, без `VK_`).

| Переменная | Умолчание | Что значит |
|---|---|---|
| `VK_XRAY_VERSION` | — | версия Xray, обязательный пин |
| `VK_XRAY_SHA256_AMD64` | — | sha256 архива для amd64 |
| `VK_XRAY_SHA256_ARM64` | — | sha256 архива для arm64 |
| `VK_TUNNEL_NAME` | — | имя туннеля, совпадает с nginx-витриной |
| `VK_XHTTP_PORT` | — | наружный HTTPS-порт XHTTP за nginx |
| `VK_XH_PORT` | — | внутренний порт XHTTP на loopback |
| `VK_XH_PATH` | — | путь XHTTP, похожий на статику |
| `VK_ENC_PORT` | — | наружный TCP-порт VLESS Encryption |
| `VK_ENC_PATH` | — | путь VLESS Encryption |
| `VK_SOCKS_PORT` | `10801` | петлевой SOCKS-порт для sing-box |
| `VK_CASCADE` | `0` | `1` — entry с маршрутизацией в каскад, `0` — solo |
| `VK_LABEL` | пусто | подпись входов в приложениях клиентов |

## Проверка

`verify.sh` проверяет:

- `xray run -test -config /usr/local/etc/xray/config.json`;
- активность службы `xray`;
- прослушивание `127.0.0.1:{socks_port}`, `127.0.0.1:{xh_port}` и `:{enc_port}`;
- в каскаде: зарубежный сайт через SOCKS видит IP выхода, российский — IP самого входа;
- петля узла `127.0.0.1:8444` через SOCKS закрыта;
- XHTTP-путь за nginx доходит до xray, а не до витрины;
- nftables-правило открывает `{enc_port}`.

Полезные команды:

```sh
/usr/local/bin/xray run -test -config /usr/local/etc/xray/config.json
systemctl status xray
ss -Hltn | grep -E '127.0.0.1:10801|127.0.0.1:{xh_port}|:{enc_port}'
curl -s -m10 -x socks5h://127.0.0.1:10801 https://example.org
nft list chain inet vpnkit_fw input
```

## Ловушки

- [P-10](../60-pitfalls.md) — версия и sha256 архива обязательны; без совпадения хеша бинарник не ставится.
- [P-16](../60-pitfalls.md) — локальный DNS/loopback должен уходить напрямую.
- [P-17](../60-pitfalls.md) — частные сети и петля узла закрыты.
- [P-18](../60-pitfalls.md) — sniffing настроен с исключениями, чтобы не ломать видео-стриминг.
- [P-19](../60-pitfalls.md) — конфиг `/usr/local/etc/xray/config.json` не правится руками: его пишет `vpnkit-gen.py`.
- [P-27](../60-pitfalls.md) — access-лог туннеля выключен.

## Откат

`rollback.sh`:

- возвращает предыдущий бинарник, если он был сохранён;
- отключает `xray` и `vpnkit-gen.timer`, если модуль ставился впервые;
- удаляет `/usr/local/bin/xray`, если бинарник был создан модулем;
- восстанавливает снимки файлов;
- перезагружает `nftables` и nginx;
- при наличии конфига и включённой службы перезапускает `xray`.
