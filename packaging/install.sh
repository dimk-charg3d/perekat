#!/bin/sh
# Installs Perekat for the current user: the programs into ~/.local/bin and
# an entry into the application menu. Run it from the unpacked folder.
set -e
cd "$(dirname "$0")"
PREFIX=${PREFIX:-$HOME/.local}
install -Dm755 perekat "$PREFIX/bin/perekat"
install -Dm755 perekat-gui "$PREFIX/bin/perekat-gui"
install -Dm644 perekat.svg "$PREFIX/share/icons/hicolor/scalable/apps/perekat.svg"
install -d "$PREFIX/share/applications"
sed "s|@BINDIR@|$PREFIX/bin|" perekat.desktop > "$PREFIX/share/applications/perekat.desktop"
update-desktop-database "$PREFIX/share/applications" 2>/dev/null || true
echo "Перекат установлен. Запуск: perekat-gui (или из списка приложений)."
