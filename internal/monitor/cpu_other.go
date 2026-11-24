//go:build !unix && !windows

package monitor

import "errors"

// processCPUSeconds 在不支持的平台上返回错误，采样器会把 CPU 使用率记为 0。
func processCPUSeconds() (float64, error) {
	return 0, errors.New("process cpu time is not available on this platform")
}
