// +build !windows

package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"

	"github.com/telepresenceio/telepresence/v2/pkg/proc"
)

const (
	// ConnectorSocketName is the path used when communicating to the connector process
	ConnectorSocketName = "/tmp/telepresence-connector.socket"

	// DaemonSocketName is the path used when communicating to the daemon process
	DaemonSocketName = "/var/run/telepresence-daemon.socket"
)

// DialSocket dials the given unix socket and returns the resulting connection
func DialSocket(ctx context.Context, socketName string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second) // FIXME(lukeshu): Make this configurable
	defer cancel()
	conn, err := grpc.DialContext(ctx, "unix:"+socketName,
		grpc.WithInsecure(),
		grpc.WithNoProxy(),
		grpc.WithBlock(),
		grpc.FailOnNonTempDialError(true),
	)
	if err != nil {
		if err == context.DeadlineExceeded {
			// grpc.DialContext doesn't wrap context.DeadlineExceeded with any useful
			// information at all.  Fix that.
			err = &net.OpError{
				Op:  "dial",
				Net: "unix",
				Addr: &net.UnixAddr{
					Name: socketName,
					Net:  "unix",
				},
				Err: fmt.Errorf("socket exists but is not responding: %w", err),
			}
		}
		// Add some Telepresence-specific commentary on what specific common errors mean.
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			err = fmt.Errorf("%w; this usually means that the process has locked up", err)
		case errors.Is(err, syscall.ECONNREFUSED):
			err = fmt.Errorf("%w; this usually means that the process has terminated ungracefully", err)
		case errors.Is(err, os.ErrNotExist):
			err = fmt.Errorf("%w; this usually means that the process is not running", err)
		}
		return nil, err
	}
	return conn, nil
}

// ListenSocket returns a listener for the given named pipe and returns the resulting connection
func ListenSocket(_ context.Context, processName, socketName string) (net.Listener, error) {
	if proc.IsAdmin() {
		origUmask := unix.Umask(0)
		defer unix.Umask(origUmask)
	}
	listener, err := net.Listen("unix", socketName)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			err = fmt.Errorf("socket %q exists so the %s is either already running or terminated ungracefully", socketName, processName)
		}
		return nil, err
	}
	// Don't have dhttp.ServerConfig.Serve unlink the socket; defer unlinking the socket
	// until the process exits.
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	return listener, nil
}

// SocketExists returns true if a socket is found at the given path
func SocketExists(path string) bool {
	s, err := os.Stat(path)
	return err == nil && s.Mode()&os.ModeSocket != 0
}
