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
	"text/tabwriter"

	"github.com/charg3d/perekat/internal/app"
	"github.com/charg3d/perekat/internal/core"
	"github.com/charg3d/perekat/internal/ipc"
)

const usage = `perekat links Arch and Windows machines on a local network.

Usage:
  perekat daemon [-v]       run in the foreground: find devices and keep links to them
  perekat devices           list connected and paired devices
  perekat refresh           look for devices again
  perekat pair <device>     ask a device to pair, or accept its request
  perekat unpair <device>   forget a device, or refuse its request
  perekat remote <device>   toggle remote control: this mouse moves that device's pointer
  perekat remote off        give the mouse back to this machine
  perekat send <device> <file>...    send files; they land in the device's downloads folder
  perekat run <device> [command]     run a command the device offers, or list them
  perekat commands          show the file that defines the commands this machine offers
  perekat ping <device>     show a notification on a device
  perekat ring <device>     make a device play a sound

<device> is a device name or id as shown by "perekat devices".
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; {
	case cmd == "daemon":
		err = daemon(args)
	case cmd == "devices" && len(args) == 0:
		err = devices()
	case cmd == "refresh" && len(args) == 0:
		err = withClient(func(c *ipc.Client) error { return c.Refresh() })
	case cmd == "pair" && len(args) == 1:
		err = pair(args[0])
	case cmd == "unpair" && len(args) == 1:
		err = unpair(args[0])
	case cmd == "remote" && len(args) == 1:
		err = remote(args[0])
	case cmd == "send" && len(args) >= 2:
		err = send(args[0], args[1:])
	case cmd == "run" && (len(args) == 1 || len(args) == 2):
		err = run(args[0], args[1:])
	case cmd == "commands" && len(args) == 0:
		err = commands()
	case cmd == "ping" && len(args) == 1:
		err = withClient(func(c *ipc.Client) error { return c.Ping(args[0]) })
	case cmd == "ring" && len(args) == 1:
		err = withClient(func(c *ipc.Client) error { return c.Ring(args[0]) })
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "perekat:", err)
		os.Exit(1)
	}
}

func daemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	verbose := fs.Bool("v", false, "log ignored packets and failed connections")
	fs.Parse(args)

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return app.Run(ctx, log)
}

func devices() error {
	client, err := ipc.NewClient()
	if err != nil {
		return err
	}
	list, err := client.Devices()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("no devices found")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tADDRESS\tSTATUS\tID")
	for _, d := range list {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", d.Name, d.Type, d.Addr, status(d), d.ID)
	}
	return w.Flush()
}

func status(d core.DeviceInfo) string {
	switch {
	case d.Pairing == "incoming":
		return "wants to pair, key " + d.Key
	case d.Pairing == "requested":
		return "waiting for it to accept, key " + d.Key
	case d.Remote:
		return "paired, under remote control"
	case d.Paired && d.Connected:
		return "paired"
	case d.Paired:
		return "paired, offline"
	default:
		return "not paired"
	}
}

func pair(device string) error {
	client, err := ipc.NewClient()
	if err != nil {
		return err
	}
	d, err := client.Pair(device)
	if err != nil {
		return err
	}
	if d.Paired {
		fmt.Printf("paired with %s\n", d.Name)
		return nil
	}
	fmt.Printf("pair request sent to %s\nconfirm on that device that it shows the key %s\n", d.Name, d.Key)
	return nil
}

func unpair(device string) error {
	client, err := ipc.NewClient()
	if err != nil {
		return err
	}
	return client.Unpair(device)
}

func remote(device string) error {
	client, err := ipc.NewClient()
	if err != nil {
		return err
	}
	if device == "off" {
		return client.RemoteOff()
	}
	active, err := client.Remote(device)
	if err != nil {
		return err
	}
	if active {
		fmt.Printf("remote control on: the mouse now moves the pointer of %s\nrun `perekat remote off` to get it back\n", device)
	} else {
		fmt.Println("remote control off")
	}
	return nil
}

func withClient(do func(*ipc.Client) error) error {
	client, err := ipc.NewClient()
	if err != nil {
		return err
	}
	return do(client)
}

func send(device string, files []string) error {
	// The daemon opens the files itself, possibly from another directory.
	for i, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return err
		}
		files[i] = abs
	}
	return withClient(func(c *ipc.Client) error { return c.Send(device, files) })
}

func run(device string, command []string) error {
	return withClient(func(c *ipc.Client) error {
		if len(command) == 1 {
			return c.Run(device, command[0])
		}
		list, err := c.Devices()
		if err != nil {
			return err
		}
		for _, d := range list {
			if d.ID != device && !strings.EqualFold(d.Name, device) {
				continue
			}
			if len(d.Commands) == 0 {
				fmt.Println("the device offers no commands")
			}
			for _, cmd := range d.Commands {
				fmt.Printf("%s\t%s\n", cmd.Name, cmd.Command)
			}
			return nil
		}
		return fmt.Errorf("no device named %q", device)
	})
}

func commands() error {
	return withClient(func(c *ipc.Client) error {
		path, err := c.CommandsPath()
		if err != nil {
			return err
		}
		fmt.Printf("Commands this machine offers to paired devices are defined in:\n  %s\n\nIt is a JSON object of name and shell command, for example:\n  {\"lock\": \"hyprlock\", \"sleep\": \"systemctl suspend\"}\n", path)
		return nil
	})
}
