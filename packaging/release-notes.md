Свой VPN на своих серверах: каскад из двух серверов (вход в России → выход за границей) или один зарубежный сервер. Мастер проверит серверы, переведёт их на ключ, поставит выбранные модули с автооткатом и выдаст подключения — QR и ссылки.

## Что скачать

| Система | Приложение с окном | Командная строка |
|---|---|---|
| Windows 10/11 (x64) | `vpnkit-app-…-windows-amd64.exe` | `vpnkit-cli-…-windows-amd64.exe` |
| Windows на ARM | `vpnkit-app-…-windows-arm64.exe` | `vpnkit-cli-…-windows-arm64.exe` |
| macOS (Intel и Apple Silicon) | `vpnkit-app-…-macos-universal.zip` | `vpnkit-cli-…-macos-arm64` / `-macos-amd64` |
| Linux x64 | `vpnkit-app-…-linux-amd64.tar.gz` | `vpnkit-cli-…-linux-amd64` |
| Linux ARM64 | `vpnkit-app-…-linux-arm64.tar.gz` | `vpnkit-cli-…-linux-arm64` |

Контрольные суммы — `SHA256SUMS`.

## Первый запуск

- **Windows:** нужен WebView2 (в Windows 10/11 уже есть). Сборка не подписана — SmartScreen предупредит: «Подробнее» → «Выполнить в любом случае».
- **macOS:** распакуйте, перенесите `vpnkit.app` в «Программы». Сборка не подписана — первый раз откройте через правый клик → «Открыть» (или `xattr -dr com.apple.quarantine /Applications/vpnkit.app`).
- **Linux:** нужны GTK3 и WebKitGTK 4.1 (подробно — `README.txt` в архиве), затем `./install.sh`.

Как пользоваться — https://github.com/REPO/blob/main/docs/USAGE.md.

Лицензия — GPL-3.0-or-later.
