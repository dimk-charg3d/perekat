//go:build !linux || phone

package desktop

// SendTarget is a device files can be sent to from the file manager.
type SendTarget struct {
	ID, Name string
}

// SetSendMenu does nothing here: only KDE's file manager is taught so far.
func SetSendMenu(targets []SendTarget) {}
