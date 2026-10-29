package execbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// The helpers run inside the sandbox as this program, started with one of
// these first arguments.
const (
	helperBridge   = "__boundedcode_sandbox_bridge"
	helperLandlock = "__boundedcode_sandbox_landlock"
)

// Main runs a helper when the process was started as one, and exits; it
// returns at once otherwise. The program's main and test mains call it
// first.
func Main() {
	if len(os.Args) < 2 {
		return
	}
	switch os.Args[1] {
	case helperBridge:
		// bridge SOCKET PORT -- argv...
		if len(os.Args) < 6 || os.Args[4] != "--" {
			fail("bridge: bad arguments")
		}
		os.Exit(runBridge(os.Args[2], os.Args[3], os.Args[5:]))
	case helperLandlock:
		if len(os.Args) < 4 || os.Args[2] != "--" {
			fail("landlock: bad arguments")
		}
		if err := runLandlockHelper(os.Args[3:]); err != nil {
			fail("landlock: " + err.Error())
		}
		os.Exit(127)
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "boundedcode sandbox: "+msg)
	os.Exit(126)
}

// runBridge listens on 127.0.0.1:port inside the sandbox's network
// namespace, forwards each connection to the proxy's Unix socket, and runs
// the command. Its exit status is the command's.
func runBridge(socket, port string, argv []string) int {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:"+port)
	if err != nil {
		fail("bridge: " + err.Error())
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				u, err := (&net.Dialer{}).DialContext(context.Background(), "unix", socket)
				if err != nil {
					return
				}
				defer u.Close()
				go func() { _, _ = io.Copy(u, c); _ = closeWrite(u) }()
				_, _ = io.Copy(c, u)
			}()
		}
	}()
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:noctx // ends with the sandbox
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 127
	}
	sig := make(chan os.Signal, 4)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for s := range sig {
			_ = cmd.Process.Signal(s)
		}
	}()
	err = cmd.Wait()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return 127
	}
	return 0
}

func closeWrite(c net.Conn) error {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}
