package backend

import (
	"context"
	"fmt"
	"net"
	"oldipmi/internal/protocol"
	"os"
	"testing"
)

// endingConsole's Run ends at once with err.
type endingConsole struct{ err error }

func (c endingConsole) Run(context.Context, func(protocol.Frame)) error { return c.err }
func (endingConsole) Key(protocol.Input) error                          { return nil }
func (endingConsole) Pointer(protocol.Input) error                      { return nil }
func (endingConsole) Release() error                                    { return nil }
func (endingConsole) Close() error                                      { return nil }

// A console that ends says why with the class of its error, and names no
// stage: its setup was done. (A failed setup names its stage too:
// sequences.json.)
func TestConsoleEndSaysItsClass(t *testing.T) {
	err := fmt.Errorf("the AMI keepalive could not be sent: %w", &net.OpError{Op: "write", Net: "tcp", Err: os.ErrDeadlineExceeded})
	peer, done := startServe(t, endingConsole{err})
	if e := readEvent(t, peer); e.Type != EventError || e.State != StateDisconnected || e.Message != err.Error() ||
		e.Code != protocol.CodeUnreachable || e.Stage != "" {
		t.Fatalf("%+v", e)
	}
	if got := <-done; got != err {
		t.Fatal(got)
	}
}
