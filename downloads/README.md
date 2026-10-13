# Готовые сборки

Последняя сборка из ветки `main`. Нажмите на файл, затем «Download raw file».

| Система | Файл | Что делать |
|---|---|---|
| Windows 10/11 (x64) | [perekat-windows.zip](perekat-windows.zip) | Распаковать, запустить `perekat-gui.exe`. При первом запуске разрешить доступ к **частным** сетям. |
| Android 8+ (arm64) | [perekat-android.apk](perekat-android.apk) | Установить, разрешить уведомления и доступ к файлам. |
| Linux (x64) | [perekat-linux.tar.gz](perekat-linux.tar.gz) | Распаковать, запустить `./install.sh`. |

Сборки не подписаны: Windows покажет предупреждение SmartScreen («Подробнее» → «Выполнить в любом случае»), Android спросит разрешение на установку из неизвестного источника.

На Windows 10 для окна нужен [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/); на Windows 11 он уже есть.

Что нужно на Linux — в [основном README](../README.md#установка).
