//go:build !darwin && !linux

package perfworkload

func processPeakRSSBytes() uint64 { return 0 }
