package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed index.html
var indexHTML string

var indexTemplate = template.Must(template.New("index.html").Parse(indexHTML))

type specSource struct {
	title string
	slug  string
	url   string
}

type preparedSpecs struct {
	root         string
	resolvedRoot string
	sources      []specSource
}

type pageData struct {
	Title  string
	Config template.JS
}

func runServer(
	ctx context.Context,
	specFiles []string,
	opts options,
	output io.Writer,
	opener browserOpener,
) error {
	if opts.port < 0 || opts.port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	if err := validateServerOverride(opts.serverOverride); err != nil {
		return err
	}

	specs, err := prepareSpecs(specFiles, opts.root)
	if err != nil {
		return err
	}

	handler, err := newHandler(specs, opts.serverOverride)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.port))
	if err != nil {
		return fmt.Errorf("listen on port %d: %w", opts.port, err)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	referenceURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	fmt.Fprintf(output, "Serving OpenAPI reference at %s\n", referenceURL)

	if !opts.noOpen {
		if err := opener(referenceURL); err != nil {
			_ = shutdown(server)
			return err
		}
	}

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve OpenAPI reference: %w", err)
	case <-ctx.Done():
		if err := shutdown(server); err != nil {
			return err
		}
		err := <-serveErr
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve OpenAPI reference: %w", err)
		}
		return nil
	}
}

func shutdown(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down server: %w", err)
	}
	return nil
}

func validateServerOverride(value string) error {
	if value == "" {
		return nil
	}

	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("server must be an absolute http or https URL")
	}
	return nil
}

func prepareSpecs(specFiles []string, rootFlag string) (preparedSpecs, error) {
	if len(specFiles) == 0 {
		return preparedSpecs{}, fmt.Errorf("at least one spec file is required")
	}

	absoluteFiles := make([]string, 0, len(specFiles))
	for _, name := range specFiles {
		extension := strings.ToLower(filepath.Ext(name))
		if extension != ".yaml" && extension != ".yml" && extension != ".json" {
			return preparedSpecs{}, fmt.Errorf("%s must be a YAML or JSON file", name)
		}

		absolute, err := filepath.Abs(name)
		if err != nil {
			return preparedSpecs{}, fmt.Errorf("resolve %s: %w", name, err)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return preparedSpecs{}, fmt.Errorf("read spec file %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return preparedSpecs{}, fmt.Errorf("%s is not a regular file", name)
		}
		absoluteFiles = append(absoluteFiles, filepath.Clean(absolute))
	}

	root, err := resolveRoot(absoluteFiles, rootFlag)
	if err != nil {
		return preparedSpecs{}, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return preparedSpecs{}, fmt.Errorf("resolve root %s: %w", root, err)
	}

	sources := make([]specSource, 0, len(absoluteFiles))
	for index, absolute := range absoluteFiles {
		relative, err := filepath.Rel(root, absolute)
		if err != nil || !isWithinRoot(relative) {
			return preparedSpecs{}, fmt.Errorf("spec file %s is outside root %s", specFiles[index], root)
		}

		resolvedFile, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return preparedSpecs{}, fmt.Errorf("resolve spec file %s: %w", specFiles[index], err)
		}
		resolvedRelative, err := filepath.Rel(resolvedRoot, resolvedFile)
		if err != nil || !isWithinRoot(resolvedRelative) {
			return preparedSpecs{}, fmt.Errorf("spec file %s resolves outside root %s", specFiles[index], root)
		}

		sources = append(sources, specSource{
			title: filepath.Base(absolute),
			slug:  fmt.Sprintf("spec-%d", index+1),
			url:   "/spec/" + escapePath(relative),
		})
	}

	return preparedSpecs{
		root:         root,
		resolvedRoot: resolvedRoot,
		sources:      sources,
	}, nil
}

func resolveRoot(files []string, rootFlag string) (string, error) {
	if rootFlag == "" {
		return commonRoot(files), nil
	}

	root, err := filepath.Abs(rootFlag)
	if err != nil {
		return "", fmt.Errorf("resolve root %s: %w", rootFlag, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("read root %s: %w", rootFlag, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root %s is not a directory", rootFlag)
	}
	return filepath.Clean(root), nil
}

func commonRoot(files []string) string {
	root := filepath.Dir(files[0])
	for _, file := range files[1:] {
		for {
			relative, err := filepath.Rel(root, file)
			if err == nil && isWithinRoot(relative) {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				break
			}
			root = parent
		}
	}
	return root
}

func isWithinRoot(relative string) bool {
	return relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(relative)
}

func escapePath(relative string) string {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func newHandler(specs preparedSpecs, serverOverride string) (http.Handler, error) {
	page, err := renderPage(specs.sources, serverOverride)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/spec/", localFileHandler(specs.root, specs.resolvedRoot))
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = writer.Write(page)
	})
	return mux, nil
}

func renderPage(sources []specSource, serverOverride string) ([]byte, error) {
	configSources := make([]map[string]string, 0, len(sources))
	for _, source := range sources {
		configSources = append(configSources, map[string]string{
			"title": source.title,
			"slug":  source.slug,
			"url":   source.url,
		})
	}

	config := map[string]any{
		"agent": map[string]bool{
			"disabled": true,
		},
		"sources": configSources,
	}
	if serverOverride != "" {
		config["servers"] = []map[string]string{
			{
				"description": "CLI override",
				"url":         serverOverride,
			},
		}
	}

	encodedConfig, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode Scalar configuration: %w", err)
	}

	title := "OpenAPI Reference"
	if len(sources) == 1 {
		title = sources[0].title
	}

	var rendered bytes.Buffer
	if err := indexTemplate.Execute(&rendered, pageData{
		Title:  title,
		Config: template.JS(encodedConfig),
	}); err != nil {
		return nil, fmt.Errorf("render index page: %w", err)
	}
	return rendered.Bytes(), nil
}

func localFileHandler(root, resolvedRoot string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		relative := strings.TrimPrefix(request.URL.Path, "/spec/")
		relative = filepath.Clean(filepath.FromSlash(relative))
		if relative == "." || !isWithinRoot(relative) {
			http.NotFound(writer, request)
			return
		}

		target := filepath.Join(root, relative)
		resolvedTarget, err := filepath.EvalSymlinks(target)
		if err != nil {
			http.NotFound(writer, request)
			return
		}
		resolvedRelative, err := filepath.Rel(resolvedRoot, resolvedTarget)
		if err != nil || !isWithinRoot(resolvedRelative) {
			http.NotFound(writer, request)
			return
		}

		info, err := os.Stat(resolvedTarget)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(writer, request)
			return
		}

		switch strings.ToLower(filepath.Ext(resolvedTarget)) {
		case ".yaml", ".yml":
			writer.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		case ".json":
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(writer, request, resolvedTarget)
	})
}
