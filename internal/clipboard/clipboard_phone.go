//go:build phone

// Package clipboard watches and sets the text clipboard.
package clipboard

import (
	"context"
	"errors"

	"github.com/charg3d/perekat/internal/phonehost"
)

// Watch calls onChange with the text the user copies, until ctx is cancelled.
// Android only lets an application see the clipboard while it is on screen.
func Watch(ctx context.Context, onChange func(string)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case text := <-phonehost.Clipboard():
			onChange(text)
		}
	}
}

// Set replaces the clipboard text.
func Set(text string) error {
	phonehost.Push(phonehost.Event{Type: "clipboard", Text: text})
	return nil
}

// OnImage is never called here: pictures in the clipboard are not read on
// this system yet.
var OnImage func(png []byte)

// SetImage reports that this system cannot take a picture into its clipboard.
func SetImage(png []byte) error { return errors.ErrUnsupported }
