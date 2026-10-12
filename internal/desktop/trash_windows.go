//go:build windows

package desktop

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	shell32              = syscall.NewLazyDLL("shell32.dll")
	procSHFileOperation  = shell32.NewProc("SHFileOperationW")
	procGetLogicalDrives = kernel32.NewProc("GetLogicalDrives")
)

// Trash moves a file or a folder to the recycle bin, from where it can be restored.
func Trash(path string) error {
	const (
		foDelete          = 3
		fofSilent         = 0x0004
		fofNoConfirmation = 0x0010
		fofAllowUndo      = 0x0040
		fofNoErrorUI      = 0x0400
	)
	// The list of paths ends with two zero characters.
	from, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0)
	op := struct {
		Hwnd                 uintptr
		Func                 uint32
		From, To             *uint16
		Flags                uint16
		AnyOperationsAborted int32
		NameMappings         uintptr
		ProgressTitle        *uint16
	}{Func: foDelete, From: &from[0], Flags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI}
	if r, _, _ := procSHFileOperation.Call(uintptr(unsafe.Pointer(&op))); r != 0 {
		return fmt.Errorf("cannot move to the recycle bin (code %d)", r)
	}
	return nil
}

// Roots lists the drives.
func Roots() []string {
	mask, _, _ := procGetLogicalDrives.Call()
	var drives []string
	for i := 0; i < 26; i++ {
		if mask&(1<<i) != 0 {
			drives = append(drives, string(rune('A'+i))+":\\")
		}
	}
	return drives
}
