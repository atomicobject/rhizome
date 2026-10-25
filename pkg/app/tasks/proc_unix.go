//go:build !windows

package tasks

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree kills a process, its process group, and every descendant. Agent
// CLIs put shell commands in process groups of their own, so the group
// alone is not enough; descendants are collected before anything is killed,
// while they are still linked to the tree.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	tree := descendants(pid)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	for _, child := range tree {
		_ = syscall.Kill(child, syscall.SIGKILL)
	}
}

// descendants lists every process below pid, from one `ps` snapshot.
func descendants(pid int) []int {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		p, err1 := strconv.Atoi(fields[0])
		pp, err2 := strconv.Atoi(fields[1])
		if err1 == nil && err2 == nil {
			children[pp] = append(children[pp], p)
		}
	}
	var all []int
	queue := []int{pid}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, c := range children[next] {
			all = append(all, c)
			queue = append(queue, c)
		}
	}
	return all
}
