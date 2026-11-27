# cascade-awg0 — обфусцированный туннель AmneziaWG между entry и exit

**Роли:** entry — клиент туннеля, exit — сервер.  
**Зависит от:** base-host, amneziawg.  
**Код:** [agent/cascade-awg0/](../../agent/cascade-awg0/)

Модуль соединяет российский сервер entry с зарубежным сервером exit через AmneziaWG и управляет резервными маршрутами. Клиенты подключаются к entry, а зарубежный трафик entry направляется через этот туннель на exit, откуда уходит в интернет.

## Как устроено

Модуль ставится только в топологии `cascade`. Порядок установки: сначала exit, затем entry.

Если на entry нет утилиты `ping`, установщик добавляет пакет `iputils-ping` перед настройкой интерфейса и фаервола.

### Резервирование

Для схемы, проверенной на паре `msk` → `ams2`, вход держит `awg0` на IPv4, переключает транспорт на IPv6 при недоступности IPv4 выхода и может перенести таблицу `200` на отдельный TCP-туннель. Адреса интерфейсов `awg0` и клиентский трафик остаются IPv4 при смене транспорта.

| Сбой | Действие на входе | Условие возврата |
|---|---|---|
| Не отвечает AWG и недоступен IPv4 выхода | При заданном `exit_ipv6` пробует AWG поверх IPv6, затем проверяет туннель. | Три успешные внешние проверки IPv4 (интервал 10 минут), затем проверка самого туннеля по IPv4. При потере IPv6 возможен досрочный возврат. |
| `awg0` не проходит три проверки подряд | При настроенном Reality и подтверждённом HTTPS-выходе через `sbtun` меняет маршрут в таблице `200`. | Восемь успешных проверок основного туннеля подряд и подтверждённый HTTPS-выход. При двух сбоях HTTPS на резерве возвращает маршрут на `awg0`. |
| Telegram недоступен по активному каналу шесть проверок подряд | При работающем `awgw` переводит только маршруты префиксов Telegram из основной таблицы на WARP. | 12 удачных проверок Telegram через каскад или три сбоя проверки WARP. |

`sbtun` создаёт sing-box на входе, который отправляет трафик в локальный SOCKS Xray. Xray подключается к выделенному VLESS Reality на выходе. Пульт скачивает пакеты и проверяет SHA-256. Установщик проверяет конфиги обоих процессов и HTTPS через TUN, сверяя публичный IPv4 выхода. Службы резерва и их конфиги отдельны от клиентских Xray и Hysteria2. Путь WARP зависит от включённого модуля [warp-ru](warp-ru.md).

Настройки резервов задаются на шаге выхода в мастере или у выхода в [inventory.cascade.example.yaml](../../inventory.cascade.example.yaml). Для IPv6 нужны рабочие глобальные адреса и UDP-маршрут между узлами. Для Reality укажите доступное с выхода внешнее TLS 1.3 / HTTP/2 имя. TCP-порт `24443` позволяет сохранить веб-витрину на `443`. Если `443` свободен на выходе, можно использовать его. Разрешите выбранные порты во внешнем фаерволе хостера.

```yaml
modules:
  cascade-awg0:
    exit_ipv6: "2001:db8::20"         # заменить адресом выхода
    reality_server_name: ""          # заполнить именем TLS-сайта
    reality_port: 24443
```

После изменения inventory устанавливайте `cascade-awg0` сначала на выходе, затем на входе. Для уже настроенного Кротом каскада используйте тот же `secrets.age`, чтобы сохранить оригинальные ключи. Ключи резервного Reality сохраняются вместе с ключами AWG. Для миграции вручную управляемых узлов потребуется импорт ключей, портов и конфигурации. Пример разворачивает переносимое ядро каскада. Исторические клиентские интерфейсы, отдельные панели, DNS-исключения и сторонние туннели действующих узлов требуют собственных настроек.

На входе `ip -4 route show default table 200` показывает активный канал, а `journalctl -u vpnkit-cascade-watch` — историю переключений. Для отдельной проверки резерва без переключения маршрута:

```sh
/usr/local/lib/vpnkit/cascade/watch /etc/vpnkit/cascade/watch.json probe sbtun
```

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

Секреты хранит пульт под `<exit>/cascade-awg0/*`; повтор установки сохраняет ключи. Явно заданный у exit `port` заменяет сохранённый порт. При смене порта разрешите новый UDP-порт у хостера и повторите `krot install <узел> cascade-awg0` сначала на exit, затем на entry. Между этими шагами связь прервётся.

## Проверка

`verify.sh` проверяет:

- `awg-quick@awg0` активен;
- интерфейс `awg0` существует.

На exit:

- `awg show awg0 listen-port` равен `VK_PORT`;
- в цепочке `inet vpnkit_fw input` есть правило `udp dport $VK_PORT accept`.

На entry:

- есть рукопожатие с exit (`awg show awg0 latest-handshakes`);
- пингуется `VK_PEER_TUN` через интерфейс `awg0`;
- HTTPS-проба сверяет адрес выхода через Cloudflare и AWS. Для `awg0` установщик сообщает предупреждение при недоступности проб. Если включён TCP-резерв, установщик требует прохождения HTTPS через `sbtun`.

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
- останавливает вновь созданные службы резерва и восстанавливает снимок;
- если файл `/etc/nftables.d/20-vpnkit-cascade.nft` больше нет — удаляет таблицу `ip vpnkit_cascade` и перезагружает `nftables`;
- если конфиг `/etc/amnezia/amneziawg/awg0.conf` вернулся из снимка — перезапускает `awg-quick@awg0`.
