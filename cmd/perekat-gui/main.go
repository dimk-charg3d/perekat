// perekat-gui is the desktop window. It talks to the daemon through the local
// API and starts one inside its own process when none is running.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/charg3d/perekat/internal/app"
	"github.com/charg3d/perekat/internal/core"
	"github.com/charg3d/perekat/internal/deploy"
	"github.com/charg3d/perekat/internal/ipc"
	"github.com/charg3d/perekat/internal/protocol"
	"github.com/charg3d/perekat/internal/ui"
)

// State is everything the window shows.
type State struct {
	Self      protocol.Identity   `json:"self"`
	Devices   []core.DeviceInfo   `json:"devices"`
	Transfers []core.TransferInfo `json:"transfers"`
	Relay     core.RelayStatus    `json:"relay"`
}

// App is the set of calls available to the window's scripts.
type App struct {
	ctx    context.Context
	client *ipc.Client
	view   string
	open   string
}

// InitialView names the page the window opens on: "", "relay", "settings" or
// "files" for the file browser of the first device that has one.
func (a *App) InitialView() string { return a.view }

func (a *App) State() (State, error) {
	self, err := a.client.Self()
	if err != nil {
		return State{}, err
	}
	devices, err := a.client.Devices()
	if err != nil {
		return State{}, err
	}
	transfers, err := a.client.Transfers()
	if err != nil {
		return State{}, err
	}
	relay, err := a.client.RelayStatus()
	if err != nil {
		return State{}, err
	}
	return State{Self: self, Devices: devices, Transfers: transfers, Relay: relay}, nil
}

// RelaySetup installs a relay server on the user's own machine and joins it.
// address is host or host:port, as for SSH.
func (a *App) RelaySetup(address, user, password string) error {
	target, err := deploy.ParseTarget(strings.TrimSpace(address))
	if err != nil {
		return err
	}
	if user = strings.TrimSpace(user); user != "" {
		target.User = user
	}
	target.Password = password
	return a.client.RelaySetup(target)
}

func (a *App) RelayJoin(code string) error { return a.client.RelayJoin(code) }

func (a *App) RelayLeave() error { return a.client.RelayLeave() }

// Copy puts text into the clipboard.
func (a *App) Copy(text string) error { return runtime.ClipboardSetText(a.ctx, text) }

// SendFiles asks which files to send and delivers them to the device. It
// returns how many were sent; zero means the dialog was dismissed.
func (a *App) SendFiles(id, title string) (int, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil || len(paths) == 0 {
		return 0, err
	}
	return len(paths), a.client.Send(id, paths)
}

// FS performs a file operation on a device, or on this one when id is "self".
func (a *App) FS(id string, req core.FSRequest) (core.FSResult, error) {
	return a.client.FS(id, req)
}

// FSPaste copies or moves files into a folder, on the same device or another.
func (a *App) FSPaste(src string, paths []string, dst, dir string, move bool) error {
	return a.client.FSPaste(ipc.Paste{Src: src, Paths: paths, Dst: dst, Dir: dir, Move: move})
}

// FSPreview prepares an office document for showing in the window.
func (a *App) FSPreview(id, path string) (core.Preview, error) { return a.client.FSPreview(id, path) }

// InitialPath is the file or folder the window was asked to open.
func (a *App) InitialPath() string { return a.open }

// FSOpen opens a file with the program this machine uses for it.
func (a *App) FSOpen(id, path string) error { return a.client.FSOpen(id, path) }

// FSUpload asks which files of this machine to put into a folder of a device
// and returns how many were picked.
func (a *App) FSUpload(id, dir, title string) (int, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil || len(paths) == 0 {
		return 0, err
	}
	return len(paths), a.client.FSPaste(ipc.Paste{Src: core.SelfDevice, Paths: paths, Dst: id, Dir: dir})
}

func (a *App) Settings() (core.Settings, error) { return a.client.Settings() }

func (a *App) SaveSettings(s core.Settings) error { return a.client.SaveSettings(s) }

// SetDeviceColor chooses the colour a device is shown in.
func (a *App) SetDeviceColor(id, color string) error {
	s, err := a.client.Settings()
	if err != nil {
		return err
	}
	s.Colors[id] = color
	return a.client.SaveSettings(s)
}

// Commands returns the commands this device offers to others.
func (a *App) Commands() (map[string]string, error) { return a.client.Commands() }

func (a *App) SaveCommands(commands map[string]string) error { return a.client.SaveCommands(commands) }

// ChooseFolder asks for a folder; an empty result means the dialog was dismissed.
func (a *App) ChooseFolder(current, title string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title, DefaultDirectory: current})
}

func (a *App) Run(id, command string) error { return a.client.Run(id, command) }

func (a *App) Ping(id string) error { return a.client.Ping(id) }

func (a *App) Ring(id string) error { return a.client.Ring(id) }

func (a *App) Refresh() error { return a.client.Refresh() }

func (a *App) Pair(id string) error {
	_, err := a.client.Pair(id)
	return err
}

func (a *App) Unpair(id string) error { return a.client.Unpair(id) }

func (a *App) Remote(id string) error {
	_, err := a.client.Remote(id)
	return err
}

func (a *App) RemoteOff() error { return a.client.RemoteOff() }

func (a *App) Quit() { runtime.Quit(a.ctx) }

// stopped is closed when the daemon inside this process has shut down.
var stopped = make(chan struct{})

// ensureDaemon starts a daemon in this process unless one already answers.
func ensureDaemon(ctx context.Context, client *ipc.Client, log *slog.Logger) error {
	if _, err := client.Self(); err == nil {
		close(stopped)
		return nil
	}
	failed := make(chan error, 1)
	go func() {
		failed <- app.Run(ctx, log)
		close(stopped)
	}()
	for range 50 {
		select {
		case err := <-failed:
			return err
		case <-time.After(100 * time.Millisecond):
		}
		if _, err := client.Self(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("daemon did not start")
}

func main() {
	view := flag.String("view", "", "page to open: relay, settings or files")
	open := flag.String("open", "", "a file or a folder of this machine to show in the file manager")
	flag.Parse()
	if *open != "" {
		*view = "files"
		if abs, err := filepath.Abs(*open); err == nil {
			*open = abs
		}
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client, err := ipc.NewClient()
	// The daemon gets to shut down properly, however the window goes away:
	// it has network folders to unmount.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	shutdown := func() {
		stop()
		select {
		case <-stopped:
		case <-time.After(3 * time.Second):
		}
	}
	go func() {
		<-ctx.Done()
		shutdown()
		os.Exit(0)
	}()
	if err == nil {
		err = ensureDaemon(ctx, client, log)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "perekat:", err)
		os.Exit(1)
	}

	a := &App{client: client, view: *view, open: *open}
	err = wails.Run(&options.App{
		Title:             "Перекат",
		Width:             980,
		Height:            660,
		MinWidth:          760,
		MinHeight:         520,
		BackgroundColour:  &options.RGBA{R: 0x16, G: 0x18, B: 0x1a, A: 0xff},
		AssetServer:       &assetserver.Options{Assets: ui.Assets(), Handler: client.Files()},
		Bind:              []any{a},
		HideWindowOnClose: true,
		OnStartup:         func(ctx context.Context) { a.ctx = ctx },
		// Closing the window keeps perekat running; launching it again brings
		// the window back.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "io.github.charg3d.perekat",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				runtime.WindowUnminimise(a.ctx)
				runtime.Show(a.ctx)
			},
		},
	})
	shutdown()
	if err != nil {
		fmt.Fprintln(os.Stderr, "perekat:", err)
		os.Exit(1)
	}
}
