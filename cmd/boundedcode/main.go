// Command boundedcode is the CLI entry point.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/akynte/boundedcode/internal/cli"
	"github.com/akynte/boundedcode/internal/execbox"
)

func main() {
	// The direct-mode sandbox starts this program as its in-sandbox helper
	// (network bridge, Landlock); Main runs it and exits.
	execbox.Main()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx, os.Args[1:])
	stop()
	os.Exit(code)
}
