#!/bin/sh
# Ставит vpnkit для текущего пользователя: ~/.local/bin, ярлык в меню и значок.
# Нужны GTK3 и WebKitGTK 4.1 — см. README.txt.
set -e
cd "$(dirname "$0")"
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor/512x512/apps"
install -m 755 vpnkit-app "$HOME/.local/bin/vpnkit-app"
install -m 644 vpnkit.png "$HOME/.local/share/icons/hicolor/512x512/apps/vpnkit.png"
sed "s|^Exec=.*|Exec=$HOME/.local/bin/vpnkit-app|" vpnkit.desktop > "$HOME/.local/share/applications/vpnkit.desktop"
command -v update-desktop-database >/dev/null && update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
echo "vpnkit установлен: ярлык «vpnkit» в меню приложений или команда ~/.local/bin/vpnkit-app"
