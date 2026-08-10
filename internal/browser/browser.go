// Package browser opens targets in the user's default browser.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type runner func(name string, args ...string) error

// Open opens target in the default browser for the current operating system.
// It returns after the browser process has started rather than waiting for it
// to exit.
func Open(target string) error {
	return open(target, runtime.GOOS, startCommand)
}

func open(target, currentGOOS string, run runner) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("open browser: target is empty")
	}
	if run == nil {
		return fmt.Errorf("open browser: runner is nil")
	}

	var name string
	var args []string
	switch currentGOOS {
	case "darwin":
		name = "open"
		args = []string{target}
	case "linux":
		name = "xdg-open"
		args = []string{target}
	case "windows":
		name = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", target}
	default:
		return fmt.Errorf("open browser: unsupported operating system %q", currentGOOS)
	}

	if err := run(name, args...); err != nil {
		return fmt.Errorf("open browser on %s with %s: %w", currentGOOS, name, err)
	}
	return nil
}

func startCommand(name string, args ...string) error {
	command := exec.Command(name, args...)
	if err := command.Start(); err != nil {
		return err
	}

	// Reap the child process without making browser startup blocking.
	go func() {
		_ = command.Wait()
	}()
	return nil
}
