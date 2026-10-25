//go:build !windows

package jsonstdio

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Harness CLIs spawn shells, tools, and MCP servers; a process group lets
// Close terminate all of them, as hook handlers and agent code execution do.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// processTree lists every descendant of the child from one `ps` snapshot.
// Claude Code runs shell commands in process groups of their own, which a
// group kill misses, and once the child exits they are reparented and can
// no longer be found; so Close takes this snapshot before closing input.
func processTree(process *os.Process) []int {
	// Stop has a three-second budget; a stalled ps must not spend it.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 == nil && err2 == nil {
			children[ppid] = append(children[ppid], pid)
		}
	}
	var tree []int
	for queue := []int{process.Pid}; len(queue) > 0; queue = queue[1:] {
		for _, child := range children[queue[0]] {
			tree = append(tree, child)
			queue = append(queue, child)
		}
	}
	return tree
}

// killProcessTree kills the child's process group and each descendant in
// tree. It also works after the leader has been reaped, while any
// descendant still holds the group.
func killProcessTree(process *os.Process, tree []int) error {
	for _, pid := range tree {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
