//go:build !linux || phone

// Package netfs shows the network folders of other devices as folders of
// this machine.
package netfs

import (
	"log/slog"

	"github.com/charg3d/perekat/internal/core"
)

// Mount does nothing here: only Linux can mount them so far.
func Mount(c *core.Core, log *slog.Logger) (unmount func()) {
	return func() {}
}
