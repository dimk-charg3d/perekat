package core

import (
	"syscall"
	"unsafe"
)

var procDiskFree = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// diskFree reports the free and the total space of the disk path is on.
func diskFree(path string) (free, total int64) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}
	var avail, all, totalFree uint64
	procDiskFree.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&all)), uintptr(unsafe.Pointer(&totalFree)))
	return int64(avail), int64(all)
}
