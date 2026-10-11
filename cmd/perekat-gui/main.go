// perekat-gui is the desktop window. It talks to the daemon through the local
// API and starts one inside its own process when none is running.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/charg3d/perekat/internal/app"
	"github.com/charg3d/perekat/internal/core"
	"github.com/charg3d/perekat/internal/ipc"
	"github.com/charg3d/perekat/internal/protocol"
)

//go:embed all:frontend
var embedded embed.FS

// State is everything the window shows.
type State struct {
	Self      protocol.Identity   `json:"self"`
	Devices   []core.DeviceInfo   `json:"devices"`
	Transfers []core.TransferInfo `json:"transfers"`
}

// App is the set of calls available to the window's scripts.
type App struct {
	ctx    context.Context
	client *ipc.Client
}

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
	return State{Self: self, Devices: devices, Transfers: transfers}, nil
}

// SendFiles asks which files to send and delivers them to the device.
func (a *App) SendFiles(id string) error {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "Отправить файлы"})
	if err != nil || len(paths) == 0 {
		return err
	}
	return a.client.Send(id, paths)
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

// ensureDaemon starts a daemon in this process unless one already answers.
func ensureDaemon(ctx context.Context, client *ipc.Client, log *slog.Logger) error {
	if _, err := client.Self(); err == nil {
		return nil
	}
	failed := make(chan error, 1)
	go func() { failed <- app.Run(ctx, log) }()
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
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client, err := ipc.NewClient()
	if err == nil {
		err = ensureDaemon(context.Background(), client, log)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "perekat:", err)
		os.Exit(1)
	}

	assets, _ := fs.Sub(embedded, "frontend")
	a := &App{client: client}
	err = wails.Run(&options.App{
		Title:             "Перекат",
		Width:             460,
		Height:            720,
		MinWidth:          380,
		MinHeight:         420,
		BackgroundColour:  &options.RGBA{R: 0x1b, G: 0x1e, B: 0x20, A: 0xff},
		AssetServer:       &assetserver.Options{Assets: assets},
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
	if err != nil {
		fmt.Fprintln(os.Stderr, "perekat:", err)
		os.Exit(1)
	}
}
