#!/bin/sh
# Ставит Крот для текущего пользователя: ~/.local/bin, ярлык в меню и значок.
# Нужны GTK3 и WebKitGTK 4.1 — см. README.txt.
set -e
cd "$(dirname "$0")"
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/applications" "$HOME/.local/share/icons/hicolor/scalable/apps"
install -m 755 krot-app "$HOME/.local/bin/krot-app"
install -m 644 krot.svg "$HOME/.local/share/icons/hicolor/scalable/apps/krot.svg"
sed "s|^Exec=.*|Exec=$HOME/.local/bin/krot-app|" krot.desktop > "$HOME/.local/share/applications/krot.desktop"
command -v update-desktop-database >/dev/null && update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
echo "Крот установлен: ярлык «Крот» в меню приложений или команда ~/.local/bin/krot-app"
