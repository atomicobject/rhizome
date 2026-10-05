package update

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func promptAdvancePin(stdin io.Reader, stdout io.Writer, pin, latest string) bool {
	if stdin == nil {
		return false
	}
	if stdout != nil {
		fmt.Fprintf(stdout, "Repo pins Rhizome %s; latest is %s. Advance pin and update? [y/N]: ", pin, latest)
	}
	answer, _ := bufio.NewReader(stdin).ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes"
}
