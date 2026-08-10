package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"
)

type options struct {
	port           int
	root           string
	serverOverride string
	noOpen         bool
}

type browserOpener func(string) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newRootCommand(openBrowser).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand(opener browserOpener) *cobra.Command {
	opts := options{}

	cmd := &cobra.Command{
		Use:   "serve-openapi <spec-file> [spec-file...]",
		Short: "Render local OpenAPI documents with Scalar",
		Long: "Serve one or more local OpenAPI YAML or JSON documents and render them " +
			"in a Scalar API Reference.",
		Example: "  serve-openapi openapi.yaml\n" +
			"  serve-openapi auth.yaml users.yaml --root .\n" +
			"  serve-openapi openapi.yaml --server https://auth.staging.setmore.com",
		Args:          cobra.MinimumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer(cmd.Context(), args, opts, cmd.OutOrStdout(), opener)
		},
	}

	cmd.Flags().IntVar(&opts.port, "port", 8080, "local HTTP port (use 0 to select a free port)")
	cmd.Flags().StringVar(&opts.root, "root", "", "root directory for local files and relative $refs")
	cmd.Flags().StringVar(&opts.serverOverride, "server", "", "override the API server used for test requests")
	cmd.Flags().BoolVar(&opts.noOpen, "no-open", false, "do not open the browser")

	return cmd
}

func openBrowser(targetURL string) error {
	var command *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", targetURL)
	case "linux":
		command = exec.Command("xdg-open", targetURL)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	default:
		return fmt.Errorf("opening a browser is not supported on %s; use --no-open", runtime.GOOS)
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
