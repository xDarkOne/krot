# base-host — основа сервера

**Роли:** entry, exit, solo. **Зависит от:** нет. **Код:** [agent/base-host/](../../agent/base-host/)

Модуль готовит чистый Ubuntu/Debian к работе с Кротом: включает пересылку пакетов в ядре, настраивает базовый nftables-фаервол, fail2ban, chrony, автообновления безопасности и при необходимости добавляет swap. Минимум: 512 МБ RAM вместе со swap и 5 ГБ диска.

## Как устроено

Устанавливает пакеты: `nftables`, `curl`, `ca-certificates`, `jq`, `python3`, `chrony`, `fail2ban`, `unattended-upgrades`, `needrestart`, `logrotate`, `age`.

**sysctl** — `/etc/sysctl.d/99-vpnkit.conf`:

- `net.ipv4.ip_forward=1`
- `net.ipv4.conf.all.rp_filter=2`
- `net.ipv4.conf.default.rp_filter=2`
- `net.core.default_qdisc=fq`
- `net.ipv4.tcp_congestion_control=bbr`
- `vm.swappiness=10`
- `vm.vfs_cache_pressure=50`
- `net.ipv4.ip_nonlocal_bind=1` — только для ролей `entry` и `solo`; для `exit` не ставится ([P-05](../60-pitfalls.md))

**Swap** — если суммарный swap меньше 1000 МБ и `swap_mb > 0`, создаётся файл `/swapfile`, а если он уже занят — `/swapfile.vpnkit`. Размер файла доводится до `swap_mb`, но не меньше 1024 МБ. Требуется свободное место на `/` больше `swap_mb + 3000` МБ. Файл добавляется в `/etc/fstab` с параметрами `sw 0 0`. Существующий swap хостера не отключается.

**Часовой пояс** — если текущий не совпадает с `timezone`, выполняется `timedatectl set-timezone`; файлы `/etc/timezone` и `/etc/localtime` предварительно снимаются в снимок.

**Журнал** — `/etc/systemd/journald.conf.d/50-vpnkit.conf` ограничивает `SystemMaxUse=200M`.

**fail2ban** — `/etc/fail2ban/jail.d/vpnkit.conf`:

- `banaction = nftables`
- `banaction_allports = nftables[type=allports]`
- jail `sshd`, `backend = systemd`, `port = ssh_port`

Запускаются службы `nftables`, `fail2ban`, `chrony`.

**Автообновления безопасности** — `/etc/apt/apt.conf.d/20auto-upgrades`:

- `APT::Periodic::Update-Package-Lists "1"`
- `APT::Periodic::Unattended-Upgrade "1"`

Автоперезагрузки нет.

**Фаервол** — таблица `inet vpnkit_fw` в `/etc/nftables.d/10-vpnkit-fw.nft`:

- `input`: `policy drop`; разрешены `established,related`, `lo`, `icmp`, `ipv6-icmp`, `tcp dport ssh_port`; `invalid` отбрасывается
- `forward`: `policy drop`; разрешены `established,related`
- `output`: `policy accept`

Файл сначала очищает только свою таблицу: `flush table inet vpnkit_fw`.  
`/etc/nftables.conf` подключает `/etc/nftables.d/*.nft` без `flush ruleset` ([P-01](../60-pitfalls.md)).  
`/etc/systemd/system/nftables.service.d/10-vpnkit-no-flush.conf` очищает `ExecStop`, чтобы остановка `nftables` не стирала чужие таблицы.

**Порт:** TCP `ssh_port`, назначение — SSH.

В каскаде модуль ставится на `exit` и `entry`. На `exit` не включается `ip_nonlocal_bind`. На `entry` и `solo` включается, потому что службы могут слушать адреса туннелей, которые появятся позже ([P-05](../60-pitfalls.md)).

## Параметры

Переменные `VK_*` готовит пульт. Те, что имеет смысл менять, задаются в inventory как `modules.<модуль>.<имя>` (имя — в нижнем регистре, без `VK_`).

| Переменная | Умолчание | Что значит |
|---|---|---|
| `ssh_port` | `22` | `VK_SSH_PORT`. Порт SSH в правилах nftables и в jail fail2ban. |
| `timezone` | `Europe/Moscow` | `VK_TIMEZONE`. Часовой пояс, который устанавливается через `timedatectl`. |
| `swap_mb` | `2048` | `VK_SWAP_MB`. Целевой размер swap, если текущий swap меньше 1000 МБ. |

Роль узла передаётся как `VK_ROLE` и обязательна: `entry`, `exit` или `solo`.

## Проверка

`verify.sh` проверяет:

- `net.ipv4.ip_forward = 1`
- `net.ipv4.tcp_congestion_control = bbr`
- `net.ipv4.ip_nonlocal_bind = 1` для ролей, отличных от `exit`
- суммарный swap не меньше 500 МБ
- активны службы `nftables`, `fail2ban`, `chrony`
- в `inet vpnkit_fw input` есть `policy drop` и правило `tcp dport ssh_port accept`
- `sshd -T` содержит `passwordauthentication no`; если нет, установка считается неуспешной, потому что вход по ключу должен быть настроен до base-host
- `ExecStop` у `nftables` не содержит `flush`
- `fail2ban-client status sshd` работает

Полезные команды:

```sh
sysctl -n net.ipv4.ip_forward net.ipv4.tcp_congestion_control
awk '/SwapTotal/{print int($2/1024)}' /proc/meminfo
systemctl is-active nftables fail2ban chrony
nft list chain inet vpnkit_fw input
sshd -T | grep -E '^passwordauthentication '
fail2ban-client status sshd
```

## Ловушки

- [P-01](../60-pitfalls.md) — `nftables.conf` не содержит `flush ruleset`; каждый файл очищает только свою таблицу.
- [P-05](../60-pitfalls.md) — на `entry` и `solo` включён `net.ipv4.ip_nonlocal_bind=1` для будущих адресов туннелей.
- [P-46](../60-pitfalls.md)
- [P-47](../60-pitfalls.md)
- [P-48](../60-pitfalls.md)

## Откат

`rollback.sh`:

- восстанавливает изменённые файлы из снимка
- отключает и удаляет созданные swap-файлы `/swapfile` или `/swapfile.vpnkit`, если они были созданы этим прогоном
- выполняет `systemctl daemon-reload`
- удаляет таблицу `inet vpnkit_fw`
- применяет `sysctl --system`
- перезапускает `fail2ban` и `systemd-journald`

Пакеты не удаляются.
