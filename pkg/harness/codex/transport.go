package codex

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"

	"github.com/atomicobject/rhizome/pkg/harness/internal/jsonstdio"
)

type stdioTransport = jsonstdio.Transport[rpcMessage]

func startStdio(ctx context.Context, binary string, args []string, cwd string) (transport, error) {
	return jsonstdio.Start[rpcMessage](ctx, "codex", binary, args, cwd)
}

func newJSONTransport(reader io.Reader, writer io.Writer, closer io.Closer) *stdioTransport {
	return jsonstdio.New[rpcMessage]("codex", reader, writer, closer)
}

func isNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}
