package semantic

import (
	"math"
	"runtime"
)

func defaultPlanConcurrency() int {
	return calcPlanConcurrency(runtime.GOMAXPROCS(0))
}

func calcPlanConcurrency(procs int) int {
	if procs <= 0 {
		return 1
	}
	if procs > math.MaxInt/2 {
		return math.MaxInt
	}
	return procs * 2
}
