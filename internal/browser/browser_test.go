package browser

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOpenSelectsPlatformCommand(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		wantName string
		wantArgs []string
	}{
		{
			name:     "macOS",
			goos:     "darwin",
			wantName: "open",
			wantArgs: []string{"https://example.com/reference"},
		},
		{
			name:     "Linux",
			goos:     "linux",
			wantName: "xdg-open",
			wantArgs: []string{"https://example.com/reference"},
		},
		{
			name:     "Windows",
			goos:     "windows",
			wantName: "rundll32",
			wantArgs: []string{"url.dll,FileProtocolHandler", "https://example.com/reference"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotName string
			var gotArgs []string
			run := func(name string, args ...string) error {
				gotName = name
				gotArgs = append([]string(nil), args...)
				return nil
			}

			if err := open("https://example.com/reference", test.goos, run); err != nil {
				t.Fatalf("open returned an error: %v", err)
			}
			if gotName != test.wantName {
				t.Errorf("command name = %q, want %q", gotName, test.wantName)
			}
			if !reflect.DeepEqual(gotArgs, test.wantArgs) {
				t.Errorf("command arguments = %#v, want %#v", gotArgs, test.wantArgs)
			}
		})
	}
}

func TestOpenRejectsEmptyTarget(t *testing.T) {
	called := false
	err := open("  \t", "linux", func(string, ...string) error {
		called = true
		return nil
	})

	if err == nil || !strings.Contains(err.Error(), "target is empty") {
		t.Fatalf("error = %v, want an empty target error", err)
	}
	if called {
		t.Fatal("runner was called for an empty target")
	}
}

func TestOpenRejectsNilRunner(t *testing.T) {
	err := open("https://example.com", "linux", nil)
	if err == nil || !strings.Contains(err.Error(), "runner is nil") {
		t.Fatalf("error = %v, want a nil runner error", err)
	}
}

func TestOpenRejectsUnsupportedOperatingSystem(t *testing.T) {
	called := false
	err := open("https://example.com", "plan9", func(string, ...string) error {
		called = true
		return nil
	})

	if err == nil || !strings.Contains(err.Error(), `unsupported operating system "plan9"`) {
		t.Fatalf("error = %v, want an unsupported operating system error", err)
	}
	if called {
		t.Fatal("runner was called for an unsupported operating system")
	}
}

func TestOpenWrapsStartupFailure(t *testing.T) {
	tests := []struct {
		goos    string
		command string
	}{
		{goos: "darwin", command: "open"},
		{goos: "linux", command: "xdg-open"},
		{goos: "windows", command: "rundll32"},
	}

	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			startupErr := errors.New("executable not found")
			err := open("https://example.com", test.goos, func(string, ...string) error {
				return startupErr
			})

			if !errors.Is(err, startupErr) {
				t.Fatalf("error = %v, want wrapped startup error", err)
			}
			for _, context := range []string{"open browser", test.goos, test.command} {
				if !strings.Contains(err.Error(), context) {
					t.Errorf("error %q does not contain %q", err, context)
				}
			}
		})
	}
}
