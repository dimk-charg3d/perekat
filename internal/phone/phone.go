// Package phone runs the daemon inside the Android application: the same
// daemon as on a computer, with the interface served to the application's
// web view over a local port and a few extra calls for the application.
package phone

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/charg3d/perekat/internal/app"
	"github.com/charg3d/perekat/internal/core"
	"github.com/charg3d/perekat/internal/discovery"
	"github.com/charg3d/perekat/internal/identity"
	"github.com/charg3d/perekat/internal/ipc"
	"github.com/charg3d/perekat/internal/phonehost"
	"github.com/charg3d/perekat/internal/ui"
)

// Run starts the daemon and prints the address of the interface as the first
// line of output, for the application to open. It returns when ctx ends.
func Run(ctx context.Context, log *slog.Logger, configDir, downloadDir, deviceName string) error {
	identity.SetDir(configDir)
	self, err := identity.LoadOrCreate(configDir)
	if err != nil {
		return err
	}
	// A new installation is named after the phone model.
	if self.Type != "phone" {
		self.Type = "phone"
		if deviceName != "" {
			self.Name = deviceName
		}
		if err := identity.Save(configDir, self); err != nil {
			return err
		}
	}
	phonehost.SetDownloadDir(downloadDir)
	// Keep the socket of the local API inside the application's own folder.
	os.Setenv("XDG_RUNTIME_DIR", configDir)
	useFixedDNS()

	// The port is chosen once, so the page stays valid across daemon restarts.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := probe.Addr().String()
	probe.Close()
	var raw [16]byte
	rand.Read(raw[:])
	token := hex.EncodeToString(raw[:])

	announced := false
	app.Extra = func(ctx context.Context, c *core.Core) error {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		if !announced {
			announced = true
			fmt.Printf("http://%s/#%s\n", addr, token)
		}
		return ipc.ServeHTTP(ctx, ln, c, token, ui.Assets(), hostRoutes(c.Refresh))
	}
	return app.Run(ctx, log)
}

// hostRoutes are the calls the application makes on its own behalf.
func hostRoutes(refresh func()) http.Handler {
	mux := http.NewServeMux()
	// next hands over one thing to do, or nothing after a while, so the
	// application can keep a request open instead of polling.
	mux.HandleFunc("GET /host/next", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		if e, ok := phonehost.Next(ctx); ok {
			json.NewEncoder(w).Encode(e)
		}
	})
	mux.HandleFunc("POST /host/clipboard", func(w http.ResponseWriter, r *http.Request) {
		var text string
		if json.NewDecoder(r.Body).Decode(&text) == nil {
			phonehost.ClipboardChanged(text)
		}
	})
	mux.HandleFunc("POST /host/battery", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Charge   int  `json:"charge"`
			Charging bool `json:"charging"`
		}
		if json.NewDecoder(r.Body).Decode(&b) == nil {
			phonehost.SetBattery(b.Charge, b.Charging)
		}
	})
	// network lists the phone's addresses, as "address/prefix length".
	mux.HandleFunc("POST /host/network", func(w http.ResponseWriter, r *http.Request) {
		var list []string
		if json.NewDecoder(r.Body).Decode(&list) != nil {
			return
		}
		SetNetworks(list)
		refresh()
	})
	return mux
}

// SetNetworks records the phone's addresses, each given as "address/prefix
// length". A prefix of 32 marks a network without broadcast, such as a VPN.
func SetNetworks(list []string) {
	nets := []*net.IPNet{}
	for _, item := range list {
		ip, ipnet, err := net.ParseCIDR(item)
		if err != nil {
			continue
		}
		if ones, bits := ipnet.Mask.Size(); ones == bits {
			nets = append(nets, &net.IPNet{IP: ip})
		} else {
			nets = append(nets, &net.IPNet{IP: ip, Mask: ipnet.Mask})
		}
	}
	discovery.SetLocalNets(nets)
}

// useFixedDNS points name lookups at public servers. A plain Linux program on
// Android finds no resolver configuration and would ask the phone itself.
func useFixedDNS() {
	servers := []string{"1.1.1.1:53", "8.8.8.8:53"}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			var lastErr error
			for _, server := range servers {
				conn, err := d.DialContext(ctx, network, server)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
}
