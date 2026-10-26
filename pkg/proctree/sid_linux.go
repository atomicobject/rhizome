package proctree

import (
	"os"
	"strconv"
	"strings"
)

// sessionOf returns pid's session from /proc/<pid>/stat, or -1. The command
// name may hold spaces and parentheses, so fields are read after the last
// ')': state, ppid, pgrp, session.
func sessionOf(pid int) int {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return -1
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return -1
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 4 {
		return -1
	}
	sid, err := strconv.Atoi(fields[3])
	if err != nil {
		return -1
	}
	return sid
}
