package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareSpecsSupportsMultipleDocuments(t *testing.T) {
	root := t.TempDir()
	first := writeTestFile(t, root, "apis/first spec.yaml", "openapi: 3.1.1\n")
	second := writeTestFile(t, root, "schemas/second.json", "{\"openapi\":\"3.1.1\"}")

	specs, err := prepareSpecs([]string{first, second}, "")
	if err != nil {
		t.Fatalf("prepareSpecs returned an error: %v", err)
	}

	if specs.root != root {
		t.Fatalf("root = %q, want %q", specs.root, root)
	}
	if len(specs.sources) != 2 {
		t.Fatalf("source count = %d, want 2", len(specs.sources))
	}
	if specs.sources[0].url != "/spec/apis/first%20spec.yaml" {
		t.Fatalf("first source URL = %q", specs.sources[0].url)
	}
	if specs.sources[1].url != "/spec/schemas/second.json" {
		t.Fatalf("second source URL = %q", specs.sources[1].url)
	}
}

func TestPrepareSpecsRejectsMissingFile(t *testing.T) {
	_, err := prepareSpecs([]string{filepath.Join(t.TempDir(), "missing.yaml")}, "")
	if err == nil || !strings.Contains(err.Error(), "read spec file") {
		t.Fatalf("error = %v, want a missing file error", err)
	}
}

func TestPrepareSpecsRejectsEntryOutsideCustomRoot(t *testing.T) {
	root := t.TempDir()
	outside := writeTestFile(t, t.TempDir(), "openapi.yaml", "openapi: 3.1.1\n")

	_, err := prepareSpecs([]string{outside}, root)
	if err == nil || !strings.Contains(err.Error(), "outside root") {
		t.Fatalf("error = %v, want an outside root error", err)
	}
}

func TestHandlerRendersScalarAndServesReferencedFiles(t *testing.T) {
	root := t.TempDir()
	entry := writeTestFile(
		t,
		root,
		"apis/openapi.yaml",
		"openapi: 3.1.1\ncomponents:\n  schemas:\n    Item:\n      $ref: ../components/item.yaml\n",
	)
	writeTestFile(t, root, "components/item.yaml", "type: object\n")

	specs, err := prepareSpecs([]string{entry}, root)
	if err != nil {
		t.Fatalf("prepareSpecs returned an error: %v", err)
	}
	handler, err := newHandler(specs, "https://auth.staging.setmore.com")
	if err != nil {
		t.Fatalf("newHandler returned an error: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	page := getBody(t, server.URL+"/")
	for _, expected := range []string{
		"https://cdn.jsdelivr.net/npm/@scalar/api-reference",
		"Scalar.createApiReference",
		"\"disabled\":true",
		"\"url\":\"/spec/apis/openapi.yaml\"",
		"\"url\":\"https://auth.staging.setmore.com\"",
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("page does not contain %q", expected)
		}
	}

	if body := getBody(t, server.URL+"/spec/apis/openapi.yaml"); !strings.Contains(body, "openapi: 3.1.1") {
		t.Fatalf("entry body = %q", body)
	}
	if body := getBody(t, server.URL+"/spec/components/item.yaml"); body != "type: object\n" {
		t.Fatalf("referenced file body = %q", body)
	}
}

func TestRenderPageSafelyEncodesConfiguration(t *testing.T) {
	page, err := renderPage([]specSource{
		{
			title: "</script><script>alert(1)</script>.yaml",
			slug:  "spec-1",
			url:   "/spec/openapi.yaml",
		},
	}, "")
	if err != nil {
		t.Fatalf("renderPage returned an error: %v", err)
	}

	rendered := string(page)
	if strings.Contains(rendered, "</script><script>alert(1)</script>") {
		t.Fatal("rendered page contains an unescaped script boundary")
	}
	if !strings.Contains(rendered, "\\u003c/script\\u003e") {
		t.Fatal("rendered page does not contain the safely encoded title")
	}
}

func TestValidateServerOverride(t *testing.T) {
	for _, valid := range []string{"", "https://example.com", "http://127.0.0.1:8080/api"} {
		if err := validateServerOverride(valid); err != nil {
			t.Errorf("validateServerOverride(%q) returned %v", valid, err)
		}
	}
	for _, invalid := range []string{"example.com", "file:///tmp/spec", "javascript:alert(1)"} {
		if err := validateServerOverride(invalid); err == nil {
			t.Errorf("validateServerOverride(%q) returned nil", invalid)
		}
	}
}

func TestRunServerOpensBrowser(t *testing.T) {
	root := t.TempDir()
	entry := writeTestFile(t, root, "openapi.yaml", "openapi: 3.1.1\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var openedURL string
	opener := func(targetURL string) error {
		openedURL = targetURL
		cancel()
		return nil
	}

	var output bytes.Buffer
	err := runServer(ctx, []string{entry}, options{port: 0}, &output, opener)
	if err != nil {
		t.Fatalf("runServer returned an error: %v", err)
	}
	if !strings.HasPrefix(openedURL, "http://127.0.0.1:") {
		t.Fatalf("opened URL = %q", openedURL)
	}
	if !strings.Contains(output.String(), openedURL) {
		t.Fatalf("output %q does not contain opened URL %q", output.String(), openedURL)
	}
}

func TestRunServerHonorsNoOpen(t *testing.T) {
	root := t.TempDir()
	entry := writeTestFile(t, root, "openapi.yaml", "openapi: 3.1.1\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opened := false
	err := runServer(
		ctx,
		[]string{entry},
		options{port: 0, noOpen: true},
		io.Discard,
		func(string) error {
			opened = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("runServer returned an error: %v", err)
	}
	if opened {
		t.Fatal("browser opener was called with --no-open")
	}
}

func TestRunServerRejectsInvalidPort(t *testing.T) {
	err := runServer(
		context.Background(),
		nil,
		options{port: 65536},
		io.Discard,
		func(string) error { return nil },
	)
	if err == nil || !strings.Contains(err.Error(), "port must be") {
		t.Fatalf("error = %v, want an invalid port error", err)
	}
}

func TestRootCommandRequiresSpecFile(t *testing.T) {
	command := newRootCommand(func(string) error { return nil })
	command.SetArgs(nil)
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)

	err := command.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "requires at least 1 arg") {
		t.Fatalf("error = %v, want a missing argument error", err)
	}
}

func writeTestFile(t *testing.T, root, name, contents string) string {
	t.Helper()
	target := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return target
}

func getBody(t *testing.T, targetURL string) string {
	t.Helper()
	response, err := http.Get(targetURL)
	if err != nil {
		t.Fatalf("GET %s: %v", targetURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned %s", targetURL, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", targetURL, err)
	}
	return string(body)
}
