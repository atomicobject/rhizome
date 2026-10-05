package claude

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"

	"github.com/atomicobject/rhizome/pkg/harness/internal/jsonstdio"
)

type transport interface {
	Send(context.Context, message) error
	Recv(context.Context) (message, error)
	BindSessionID(string)
	Close(context.Context) error
}

type transportStarter func(context.Context, string, []string, string) (transport, error)

type stdioTransport = jsonstdio.Transport[message]

func startStdio(ctx context.Context, binary string, args []string, cwd string) (transport, error) {
	return jsonstdio.Start[message](ctx, "claude", binary, args, cwd)
}

func newJSONTransport(reader io.Reader, writer io.Writer, closer io.Closer) *stdioTransport {
	return jsonstdio.New[message]("claude", reader, writer, closer)
}

func isNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}
