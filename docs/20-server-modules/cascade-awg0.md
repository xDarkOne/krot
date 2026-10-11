# cascade-awg0 — обфусцированный туннель AmneziaWG между entry и exit

**Роли:** entry — клиент туннеля, exit — сервер.  
**Зависит от:** base-host, amneziawg.  
**Код:** [agent/cascade-awg0/](../../agent/cascade-awg0/)

Модуль соединяет российский сервер entry с зарубежным сервером exit одним туннелем. Клиенты подключаются к entry, а зарубежный трафик entry направляется через этот туннель на exit, откуда уходит в интернет.

## Как устроено

Модуль ставится только в топологии `cascade`. Порядок установки: сначала exit, затем entry.

### Файлы

- `/etc/amnezia/amneziawg/awg0.conf` — конфиг туннеля, права `600`.
- `/etc/nftables.d/20-vpnkit-cascade.nft` — правила фаервола модуля, права `644`.
- Каталог `/etc/amnezia/amneziawg` создаётся с правами `700`, если его ещё нет.

### Служба

- `awg-quick@awg0` — systemd-юнит туннеля.
- Модуль требует наличие `awg-quick` (ставится модулем `amneziawg`).

### Конфиг awg0

Общие для entry и exit:

- `Address = $VK_ADDR`
- `Table = off`
- `MTU = $VK_MTU`
- `PrivateKey = $VK_PRIVATE`
- строка обфускации `$VK_OBFS`
- `HeaderProtectionKey = $VK_HPK`
- `ContentPaddingAddition = 0-16`
- `[Peer] PublicKey = $VK_PEER_PUBLIC`
- `PresharedKey = $VK_PSK`
- `AdvancedSecurity = on`
- `AllowedIPs = 0.0.0.0/0`

На exit дополнительно:

- `ListenPort = $VK_PORT`

На entry дополнительно:

- `Endpoint = $VK_ENDPOINT`
- `PersistentKeepalive = 25`
- `PostUp` / `PostDown` правила маршрутизации:
  - `ip rule add from <tunip> table 200 priority 200`
  - `ip rule add fwmark 0x3 table 200 priority 207`
  - `ip route replace default dev awg0 table 200`
  - для каждого префикса из `$VK_TG_PREFIXES`: `ip route replace <prefix> dev awg0`

`Table = off` обязателен: при `AllowedIPs = 0.0.0.0/0` без него awg-quick может перехватить маршрут по умолчанию.

### Правила nftables

Модуль пишет файл `/etc/nftables.d/20-vpnkit-cascade.nft` и применяет весь `/etc/nftables.conf`.

Общая таблица `inet vpnkit_fw`:

- на exit:
  - `input`: `udp dport $VK_PORT accept`, `iifname "awg0" accept`
  - `forward`: `iifname "awg0" oifname != "awg0" accept`
- на entry:
  - `input`: `iifname "awg0" accept`

Собственная таблица `ip vpnkit_cascade`:

- `postrouting`:
  - exit: `ip saddr $VK_TUN_NET oifname != "awg0" masquerade`
  - entry: `meta mark 0x00000003 oifname "awg0" masquerade`
- `mss`:
  - `iifname "awg0" tcp flags syn tcp option maxseg size set rt mtu`
  - `oifname "awg0" tcp flags syn tcp option maxseg size set rt mtu`

### Порты

- exit открывает UDP-порт `$VK_PORT` для awg0.
- entry порт не слушает, использует случайный локальный порт.

### Поведение в каскаде

- **exit** принимает туннель от entry, выпускает его трафик в интернет и маскирует адреса сети туннеля при уходе наружу.
- **entry** подключается к exit, направляет в туннель маршрут по умолчанию из таблицы `200` и перечисленные префиксы.
- На одном сервере (solo) модуль не применяется: он работает только в паре entry/exit.

## Параметры

Переменные `VK_*` готовит пульт. Те, что имеет смысл менять, задаются в inventory как `modules.<модуль>.<имя>` (имя — в нижнем регистре, без `VK_`).

| Переменная | Умолчание | Что значит |
|---|---|---|
| `tunnel_net` | внутренний /30 из манифеста | Сеть туннеля между entry и exit. Передаётся как `VK_TUN_NET`, задаётся у exit. |
| `port` | случайный 20000–60000 | UDP-порт awg0 на exit. Передаётся как `VK_PORT`, задаётся у exit. |
| `VK_ADDR` | задаётся пультом | Локальный адрес интерфейса `awg0` на текущем узле. |
| `VK_MTU` | задаётся пультом | MTU туннеля. |
| `VK_PRIVATE` | генерируется пультом | Приватный ключ текущего узла. |
| `VK_PEER_PUBLIC` | генерируется пультом | Публичный ключ второго узла. |
| `VK_PSK` | генерируется пультом | PresharedKey для `[Peer]`. |
| `VK_HPK` | генерируется пультом | HeaderProtectionKey. |
| `VK_OBFS` | генерируется пультом | Строка параметров обфускации; должна совпадать на обоих концах. |
| `VK_ENDPOINT` | `<exit-ip>:<exit-port>` | Точка входа на entry. Задаётся только для entry. |
| `VK_TG_PREFIXES` | пусто | Список префиксов, которые entry направляет в `awg0` отдельными маршрутами. |

Секреты хранит пульт под `<exit>/cascade-awg0/*`; повтор установки даёт те же значения.

## Проверка

`verify.sh` проверяет:

- `awg-quick@awg0` активен;
- интерфейс `awg0` существует.

На exit:

- `awg show awg0 listen-port` равен `VK_PORT`;
- в цепочке `inet vpnkit_fw input` есть правило `udp dport $VK_PORT accept`.

На entry:

- есть рукопожатие с exit (`awg show awg0 latest-handshakes`);
- пингуется `VK_PEER_TUN`;
- `curl -s -m10 --interface awg0 https://api.ipify.org` возвращает `VK_EXIT_IP`.

Полезные команды:

```sh
systemctl status awg-quick@awg0
ip -br addr show awg0
awg show awg0 listen-port
awg show awg0 latest-handshakes
nft list chain inet vpnkit_fw input
nft list table ip vpnkit_cascade
```

## Ловушки

- [P-30](../60-pitfalls.md) — не менять `listen-port` на живом интерфейсе командой `awg set … listen-port`.

## Откат

`rollback.sh`:

- если туннель создавался этим прогоном — останавливает и отключает `awg-quick@awg0`;
- восстанавливает снимок;
- если файл `/etc/nftables.d/20-vpnkit-cascade.nft` больше нет — удаляет таблицу `ip vpnkit_cascade` и перезагружает `nftables`;
- если конфиг `/etc/amnezia/amneziawg/awg0.conf` вернулся из снимка — перезапускает `awg-quick@awg0`.
