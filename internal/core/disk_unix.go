//go:build !windows

package core

import "syscall"

// diskFree reports the free and the total space of the disk path is on.
func diskFree(path string) (free, total int64) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return 0, 0
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize)
}
