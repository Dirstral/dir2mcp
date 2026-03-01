//go:build !windows

package host

func processAliveWindows(pid int) bool {
	_ = pid
	return false
}
