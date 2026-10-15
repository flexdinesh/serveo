// Package cmd defines the serveo command-line interface.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/flexdinesh/serveo/internal/browser"
	"github.com/flexdinesh/serveo/internal/specs"
	"github.com/flexdinesh/serveo/internal/web"
	"github.com/spf13/cobra"
)

type options struct {
	port           int
	depth          int
	root           string
	serverOverride string
	noOpen         bool
}

type runner func(context.Context, []string, options, io.Writer, io.Writer) error
type browserOpener func(string) error

// Execute runs the root command until the server exits or receives a signal.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runCommand := func(ctx context.Context, files []string, opts options, stdout, stderr io.Writer) error {
		return run(ctx, files, opts, stdout, stderr, browser.Open)
	}
	return newRootCommand(runCommand).ExecuteContext(ctx)
}

func newRootCommand(runCommand runner) *cobra.Command {
	var opts options
	command := &cobra.Command{
		Use:   "serveo <path> [spec-file...]",
		Short: "Render local OpenAPI documents with Scalar",
		Long: "Discover OpenAPI documents in one directory, or serve one or more explicitly " +
			"selected YAML or JSON documents, and render them in a Scalar API Reference.",
		Example: "  serveo .\n" +
			"  serveo openapi.yaml\n" +
			"  serveo auth.yaml users.yaml --root .\n" +
			"  serveo openapi.yaml --server https://api.example.com",
		Args:          cobra.MinimumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, args []string) error {
			if opts.port < 0 || opts.port > 65535 {
				return errors.New("--port must be between 0 and 65535")
			}
			if opts.depth < 0 {
				return errors.New("--depth must not be negative")
			}
			if err := validateServerOverride(opts.serverOverride); err != nil {
				return err
			}
			return runCommand(command.Context(), args, opts, command.OutOrStdout(), command.ErrOrStderr())
		},
	}

	command.Flags().IntVar(&opts.port, "port", 0, "local port; 0 selects a free port")
	command.Flags().IntVar(&opts.depth, "depth", 3, "maximum nested directory depth when discovering specs")
	command.Flags().StringVar(&opts.root, "root", "", "root directory for local files and relative $refs")
	command.Flags().StringVar(&opts.serverOverride, "server", "", "override the API server used for test requests")
	command.Flags().BoolVar(&opts.noOpen, "no-open", false, "do not open the browser automatically")
	return command
}

func run(
	ctx context.Context,
	files []string,
	opts options,
	stdout, stderr io.Writer,
	opener browserOpener,
) error {
	collection, err := prepareCollection(ctx, files, opts)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opts.port)))
	if err != nil {
		return fmt.Errorf("listen on local port %d: %w", opts.port, err)
	}

	app, err := web.New(web.Config{
		Specs:          collection,
		ServerOverride: opts.serverOverride,
		AllowedHost:    listener.Addr().String(),
	})
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("create web interface: %w", err)
	}

	server := &http.Server{
		Handler:           app,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()

	referenceURL := "http://" + listener.Addr().String() + "/"
	_, _ = fmt.Fprintf(stdout, "Serving OpenAPI from %s\n%s\n", collection.RootPath(), referenceURL)
	if !opts.noOpen && ctx.Err() == nil {
		if err := opener(referenceURL); err != nil {
			_, _ = fmt.Fprintf(stderr, "Could not open browser: %v\n", err)
		}
	}

	select {
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve OpenAPI reference: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("stop local server: %w", err)
		}
		if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve OpenAPI reference: %w", err)
		}
		return nil
	}
}

func prepareCollection(ctx context.Context, paths []string, opts options) (*specs.Collection, error) {
	if len(paths) == 1 {
		info, err := os.Stat(paths[0])
		if err == nil && info.IsDir() {
			entries, err := specs.Discover(ctx, paths[0], opts.depth)
			if err != nil {
				return nil, err
			}
			root := opts.root
			if root == "" {
				root = paths[0]
			}
			return specs.PrepareEntries(entries, root)
		}
		return specs.Prepare(paths, opts.root)
	}

	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return nil, errors.New("a directory may only be supplied as the sole path")
		}
	}
	return specs.Prepare(paths, opts.root)
}

func validateServerOverride(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("--server must be an absolute http or https URL")
	}
	return nil
}
