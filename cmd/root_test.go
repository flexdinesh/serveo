package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootCommandDefaults(t *testing.T) {
	var gotFiles []string
	var gotOptions options
	command := newRootCommand(func(_ context.Context, files []string, opts options, _, _ io.Writer) error {
		gotFiles = append([]string(nil), files...)
		gotOptions = opts
		return nil
	})
	command.SetArgs([]string{"openapi.yaml"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotFiles, ",") != "openapi.yaml" {
		t.Fatalf("files = %v", gotFiles)
	}
	if gotOptions.port != 0 || gotOptions.depth != 3 || gotOptions.root != "" || gotOptions.serverOverride != "" || gotOptions.noOpen {
		t.Fatalf("default options = %+v", gotOptions)
	}
}

func TestRootCommandParsesFlags(t *testing.T) {
	var gotFiles []string
	var gotOptions options
	command := newRootCommand(func(_ context.Context, files []string, opts options, _, _ io.Writer) error {
		gotFiles = append([]string(nil), files...)
		gotOptions = opts
		return nil
	})
	command.SetArgs([]string{
		"auth.yaml", "users.json", "--port", "8080", "--depth", "5", "--root", "apis",
		"--server", "https://api.example.com", "--no-open",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotFiles, ",") != "auth.yaml,users.json" {
		t.Fatalf("files = %v", gotFiles)
	}
	if gotOptions.port != 8080 || gotOptions.depth != 5 || gotOptions.root != "apis" || gotOptions.serverOverride != "https://api.example.com" || !gotOptions.noOpen {
		t.Fatalf("parsed options = %+v", gotOptions)
	}
}

func TestRootCommandRejectsInvalidArguments(t *testing.T) {
	tests := [][]string{
		nil,
		{"openapi.yaml", "--port", "-1"},
		{"openapi.yaml", "--port", "65536"},
		{"openapi.yaml", "--depth", "-1"},
		{"openapi.yaml", "--server", "example.com"},
		{"openapi.yaml", "--server", "file:///tmp/api.yaml"},
	}
	for _, args := range tests {
		command := newRootCommand(func(context.Context, []string, options, io.Writer, io.Writer) error {
			t.Fatal("runner called for invalid arguments")
			return nil
		})
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("Execute(%v) error = nil", args)
		}
	}
}

func TestPrepareCollectionDiscoversDirectory(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "services", "users", "openapi.yaml")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("openapi: 3.1.1\ninfo:\n  title: Users API\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	collection, err := prepareCollection(context.Background(), []string{root}, options{depth: 3})
	if err != nil {
		t.Fatalf("prepareCollection() error = %v", err)
	}
	if got := collection.RootPath(); got != root {
		t.Fatalf("RootPath() = %q, want %q", got, root)
	}
	sources := collection.Sources()
	if len(sources) != 1 || sources[0].Title != "Users API" || sources[0].URL != "/spec/services/users/openapi.yaml" {
		t.Fatalf("Sources() = %#v", sources)
	}
}

func TestPrepareCollectionUsesExplicitRootForDiscovery(t *testing.T) {
	root := t.TempDir()
	discoveryRoot := filepath.Join(root, "apis")
	entry := filepath.Join(discoveryRoot, "openapi.yaml")
	if err := os.MkdirAll(discoveryRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("openapi: 3.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	collection, err := prepareCollection(context.Background(), []string{discoveryRoot}, options{depth: 3, root: root})
	if err != nil {
		t.Fatalf("prepareCollection() error = %v", err)
	}
	if got := collection.RootPath(); got != root {
		t.Fatalf("RootPath() = %q, want %q", got, root)
	}
	if got := collection.Sources()[0].URL; got != "/spec/apis/openapi.yaml" {
		t.Fatalf("source URL = %q", got)
	}
}

func TestPrepareCollectionRejectsDirectoriesAmongMultiplePaths(t *testing.T) {
	firstDirectory := t.TempDir()
	secondDirectory := t.TempDir()
	file := writeSpec(t)

	for _, paths := range [][]string{
		{firstDirectory, file},
		{firstDirectory, secondDirectory},
	} {
		if _, err := prepareCollection(context.Background(), paths, options{depth: 3}); err == nil || !strings.Contains(err.Error(), "sole path") {
			t.Fatalf("prepareCollection(%v) error = %v", paths, err)
		}
	}
}

func TestRunSelectsFreePortAndStopsWithContext(t *testing.T) {
	entry := writeSpec(t)
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer
	err := run(ctx, []string{entry}, options{port: 0}, &stdout, &stderr, func(target string) error {
		if !strings.HasPrefix(target, "http://127.0.0.1:") || !strings.HasSuffix(target, "/") {
			t.Fatalf("browser target = %q", target)
		}
		cancel()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), filepath.Dir(entry)) || !strings.Contains(stdout.String(), "http://127.0.0.1:") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunReportsBrowserFailureAndContinues(t *testing.T) {
	entry := writeSpec(t)
	ctx, cancel := context.WithCancel(context.Background())
	browserErr := errors.New("browser unavailable")
	var stderr bytes.Buffer
	err := run(ctx, []string{entry}, options{port: 0}, io.Discard, &stderr, func(string) error {
		cancel()
		return browserErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), browserErr.Error()) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunHonorsNoOpen(t *testing.T) {
	entry := writeSpec(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opened := false
	err := run(ctx, []string{entry}, options{port: 0, noOpen: true}, io.Discard, io.Discard, func(string) error {
		opened = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened {
		t.Fatal("browser opener was called")
	}
}

func TestRunReportsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	err = run(context.Background(), []string{writeSpec(t)}, options{port: port, noOpen: true}, io.Discard, io.Discard, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "listen on local port") {
		t.Fatalf("run() error = %v", err)
	}
}

func TestValidateServerOverride(t *testing.T) {
	for _, value := range []string{"", "https://example.com", "http://127.0.0.1:8080/api"} {
		if err := validateServerOverride(value); err != nil {
			t.Errorf("validateServerOverride(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"example.com", "file:///tmp/spec", "javascript:alert(1)"} {
		if err := validateServerOverride(value); err == nil {
			t.Errorf("validateServerOverride(%q) error = nil", value)
		}
	}
}

func writeSpec(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(target, []byte("openapi: 3.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}
