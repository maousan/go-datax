//go:build unix

package monitor

import "syscall"

// processCPUSeconds 返回当前进程累计占用的用户态与内核态 CPU 时间（秒）。
func processCPUSeconds() (float64, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, err
	}
	return float64(usage.Utime.Sec+usage.Stime.Sec) +
		float64(usage.Utime.Usec+usage.Stime.Usec)/1e6, nil
}
