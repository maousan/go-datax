//go:build windows

package monitor

import "syscall"

// processCPUSeconds 返回当前进程累计占用的内核态与用户态 CPU 时间（秒）。
func processCPUSeconds() (float64, error) {
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, err
	}

	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, err
	}
	return filetimeSeconds(kernel) + filetimeSeconds(user), nil
}

// filetimeSeconds 将 FILETIME（100 纳秒为单位）换算成秒。
func filetimeSeconds(value syscall.Filetime) float64 {
	ticks := uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
	return float64(ticks) / 1e7
}
