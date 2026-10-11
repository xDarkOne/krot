# amneziawg — модуль ядра AmneziaWG 3.1 и утилиты AWG

**Роли:** entry, exit, solo. **Зависит от:** base-host. **Код:** [agent/amneziawg/](../../agent/amneziawg/)

Модуль нужен, чтобы сервер мог создавать и обслуживать интерфейсы AmneziaWG: он ставит ядро-модуль `amneziawg` через DKMS и утилиты `awg`/`awg-quick`. Ставится автоматически, если выбран каскад или вход AmneziaWG.

## Как устроено
- **Модуль ядра:** исходники клонируются по тегу `VK_TAG` во временный каталог `$VK_RUN/build/kmod`; версия DKMS и `WIREGUARD_VERSION` в `src/version.h` задаются тегом без `v`. Сборка и установка через DKMS: `make -C .../src dkms-install`, `dkms add`, `dkms build`, `dkms install --force` для каждого ядра.
- **Ядра:** модуль собирается для работающего ядра и всех установленных ядер, у которых есть `/lib/modules/<kernel>/build/include`. Если сборка не удалась для работающего ядра — установка прерывается; для остальных — предупреждение.
- **Совместимость:** при наличии патча `files/ubuntu-udp-tunnel-sk.patch` он накладывается, если ложится на исходники; патч подставляет новую сигнатуру `setup_udp_tunnel_sock` по заголовкам ядра. Если в новом теге уже исправлено — сборка без патча.
- **Старые версии DKMS:** для зарегистрированных версий `amneziawg` с `AUTOINSTALL=yes` (кроме текущей) `AUTOINSTALL` меняется на `no`, чтобы автосборка старых версий не роняла установку новых ядер.
- **Файлы модуля:** снимок делается для `/usr/src/amneziawg-<want>` и старых файлов `/lib/modules/<kernel>/updates/dkms/amneziawg.ko[.zst|.xz|.gz]`; после успешной установки старые файлы удаляются.
- **Утилиты:** исходники клонируются по тегу `VK_TOOLS_TAG` в `$VK_RUN/build/tools`, собираются и ставятся через `make install DESTDIR=... PREFIX=/usr WITH_WGQUICK=yes WITH_SYSTEMDUNITS=yes WITH_BASHCOMPLETION=yes`. Файлы копируются по точному списку из staging-каталога; каждый файл снимается в snapshot.
- **Установленные пути/службы:** `/usr/src/amneziawg-*`, `/usr/bin/awg`, `/usr/bin/awg-quick`, `/usr/lib/systemd/system/awg-quick@.service` (verify также допускает `/lib/systemd/system/awg-quick@.service`).
- **Пакеты:** `build-essential`, `dkms`, `git`, `libmnl-dev`, `pkg-config`, `linux-headers-$(uname -r)`.
- **Порты/nftables:** не используются (`ports: []`, `nft: {}`, `ip_rules: []`).
- **Каскад/solo:** скрипты одинаковы для всех ролей. В манифесте место в каскаде: `exit`, `entry`; в solo: `solo`. Модуль ставится на каждый узел, которому нужен AWG.

## Параметры

Переменные `VK_*` готовит пульт. Те, что имеет смысл менять, задаются в inventory как `modules.<модуль>.<имя>` (имя — в нижнем регистре, без `VK_`).
| Переменная | Умолчание | Что значит |
|---|---|---|
| `module_tag` (`VK_TAG`) | `v3.1.20260906` (в inventory — «пин пульта») | Тег модуля ядра AmneziaWG. |
| `tools_tag` (`VK_TOOLS_TAG`) | `v3.1.20260812` (в inventory — «пин пульта») | Тег утилит `awg`/`awg-quick`. |

## Проверка
`verify.sh` проверяет:
- `modinfo -F version amneziawg` совпадает с ожидаемым тегом без `v`;
- `dkms status -m amneziawg -v <want> -k $(uname -r)` показывает `installed`;
- для всех установленных ядер с заголовками есть `installed` (иначе предупреждение);
- `awg --version` содержит тег утилит;
- есть `awg-quick` и файл `awg-quick@.service`;
- создаётся тестовый интерфейс `vk-awgtest type amneziawg`, задаются параметры 3.1 (`jc 4 jmin 32 jmax 96 s1 10 s2 20 h1 100-200 h2 300-400 h3 500-600 h4 700-800`), затем интерфейс удаляется.

Полезные команды:
```sh
modinfo amneziawg | awk '/^version/{print $2}'
dkms status amneziawg
awg --version
systemctl cat awg-quick@.service
```

## Ловушки
- [P-12](../60-pitfalls.md) — версия в DKMS должна быть реальным тегом, а не «1.0.0».
- [P-13](../60-pitfalls.md) — указан в манифесте модуля; детали — в справочнике ловушек.

## Откат
`rollback.sh`:
- удаляет тестовый интерфейс `vk-awgtest`, если он остался;
- если в `created.list` есть `dkms amneziawg/<want>`, выгружает `rmmod amneziawg` только если нет активных интерфейсов `type amneziawg` и удаляет версию DKMS `--all`;
- восстанавливает snapshot файлов;
- выполняет `systemctl daemon-reload` и `depmod -a` для всех ядер в `/lib/modules/*/modules.dep`;
- пакеты сборки (`build-essential`, `dkms`, `git`, `libmnl-dev`, `pkg-config`, `linux-headers-*`) не удаляются — они безвредны.
