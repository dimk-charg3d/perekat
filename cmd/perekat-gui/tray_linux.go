package main

import (
	_ "embed"

	"fyne.io/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed tray.png
var trayIcon []byte

// startTray puts perekat into the system tray, so that the window, once
// closed, can be brought back, and perekat can be quit for good.
func startTray(a *App) {
	go systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("Перекат")
		systray.SetTooltip("Перекат")
		show := func() {
			runtime.WindowUnminimise(a.ctx)
			runtime.Show(a.ctx)
		}
		systray.SetOnTapped(show)
		open := systray.AddMenuItem("Открыть Перекат", "")
		files := systray.AddMenuItem("Проводник", "")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Выход", "")
		for {
			select {
			case <-open.ClickedCh:
				show()
			case <-files.ClickedCh:
				show()
				runtime.WindowExecJS(a.ctx, "show({ kind: 'files', id: 'self', path: '' })")
			case <-quit.ClickedCh:
				runtime.Quit(a.ctx)
				return
			}
		}
	}, nil)
}
