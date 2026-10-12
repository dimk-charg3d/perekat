//go:build phone

package input

import "errors"

var errUnsupported = errors.New("a phone cannot be controlled with a mouse, and has none to share")

// NewInjector is not available on a phone.
func NewInjector() (Injector, error) { return nil, errUnsupported }

// StartCapture is not available on a phone.
func StartCapture(emit func(Event)) (stop func(), err error) { return nil, errUnsupported }

// Supported says whether this build can capture and replay mouse input.
const Supported = false
