//go:build windows

// Package clipboard watches and sets the text clipboard.
package clipboard

import (
	"context"
	"errors"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormat    = user32.NewProc("RegisterClipboardFormatW")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	pollInterval  = 300 * time.Millisecond
)

// Watch calls onChange with the clipboard text now and after every change,
// until ctx is cancelled. Entries a password manager marks as secret and
// entries without text are skipped.
func Watch(ctx context.Context, onChange func(string)) error {
	// Password managers add this format to keep an entry out of clipboard
	// history and sync.
	name, _ := syscall.UTF16PtrFromString("ExcludeClipboardContentFromMonitorProcessing")
	secret, _, _ := procRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(name)))

	var last uintptr
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		seq, _, _ := procGetClipboardSequenceNumber.Call()
		if seq != last {
			last = seq
			if hidden, _, _ := procIsClipboardFormatAvailable.Call(secret); hidden == 0 {
				if text, ok := read(); ok {
					onChange(text)
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// open retries because another program may hold the clipboard for a moment.
func open() bool {
	for range 10 {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func read() (string, bool) {
	if !open() {
		return "", false
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return "", false
	}
	defer procGlobalUnlock.Call(h)
	var chars []uint16
	for ptr := unsafe.Pointer(p); *(*uint16)(ptr) != 0; ptr = unsafe.Add(ptr, 2) {
		chars = append(chars, *(*uint16)(ptr))
	}
	return string(utf16.Decode(chars)), true
}

// Set replaces the clipboard text.
func Set(text string) error {
	chars, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(chars)*2))
	if h == 0 {
		return err
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return err
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(chars)), chars)
	procGlobalUnlock.Call(h)

	if !open() {
		procGlobalFree.Call(h)
		return errors.New("clipboard is busy")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	// On success the system owns the memory.
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return err
	}
	return nil
}
