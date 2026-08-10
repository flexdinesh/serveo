// Package web serves a Scalar API reference for prepared local OpenAPI files.
package web

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/flexdinesh/serve-openapi/internal/specs"
)

const (
	scalarURL             = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.64.1"
	scalarIntegrity       = "sha384-SmFRDuBBmEoCbbBCTcn8/+cIQONyH0xgOKpsp6OttBlI8uG9uc6iAO8SonYQ3e1W"
	contentSecurityPolicy = "default-src 'none'; " +
		"script-src 'self' https://cdn.jsdelivr.net; " +
		"connect-src 'self' http: https:; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob: http: https:; " +
		"font-src 'self' data: https:; " +
		"worker-src blob:; " +
		"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

//go:embed index.html
var indexHTML string

//go:embed app.js
var appJS []byte

var indexTemplate = template.Must(template.New("index.html").Parse(indexHTML))

// Config describes the local reference served by an App.
type Config struct {
	Specs          *specs.Collection
	ServerOverride string
	AllowedHost    string
}

// App is an HTTP handler for a prepared OpenAPI reference.
type App struct {
	handler     http.Handler
	specs       *specs.Collection
	allowedHost string
	page        []byte
}

type pageData struct {
	Title           string
	Configuration   string
	ScalarURL       string
	ScalarIntegrity string
}

type scalarConfiguration struct {
	Agent   scalarAgent    `json:"agent"`
	Sources []scalarSource `json:"sources"`
	Servers []scalarServer `json:"servers,omitempty"`
}

type scalarAgent struct {
	Disabled bool `json:"disabled"`
}

type scalarSource struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
	URL   string `json:"url"`
}

type scalarServer struct {
	Description string `json:"description"`
	URL         string `json:"url"`
}

// New constructs a host-restricted Scalar web application.
func New(config Config) (*App, error) {
	if config.Specs == nil {
		return nil, errors.New("spec collection is nil")
	}
	if config.AllowedHost == "" {
		return nil, errors.New("allowed host is empty")
	}

	page, err := renderPage(config.Specs.Sources(), config.ServerOverride)
	if err != nil {
		return nil, err
	}

	app := &App{
		specs:       config.Specs,
		allowedHost: config.AllowedHost,
		page:        page,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", app.serveIndex)
	mux.HandleFunc("GET /assets/app.js", app.serveJavaScript)
	mux.HandleFunc("GET /spec/{path...}", app.serveSpec)
	app.handler = mux
	return app, nil
}

// ServeHTTP applies local-server security headers and dispatches a request.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w.Header())
	if !strings.EqualFold(r.Host, a.allowedHost) {
		http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
		return
	}
	a.handler.ServeHTTP(w, r)
}

func (a *App) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(a.page)
}

func (a *App) serveJavaScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(appJS)
}

func (a *App) serveSpec(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")
	target, err := a.specs.Resolve(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch strings.ToLower(filepath.Ext(target)) {
	case ".yaml", ".yml":
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	default:
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, target)
}

func renderPage(sources []specs.Source, serverOverride string) ([]byte, error) {
	configuration := scalarConfiguration{
		Agent:   scalarAgent{Disabled: true},
		Sources: make([]scalarSource, len(sources)),
	}
	for i, source := range sources {
		configuration.Sources[i] = scalarSource{
			Title: source.Title,
			Slug:  source.Slug,
			URL:   source.URL,
		}
	}
	if serverOverride != "" {
		configuration.Servers = []scalarServer{{
			Description: "CLI override",
			URL:         serverOverride,
		}}
	}

	encoded, err := json.Marshal(configuration)
	if err != nil {
		return nil, fmt.Errorf("encode Scalar configuration: %w", err)
	}
	title := "OpenAPI Reference"
	if len(sources) == 1 {
		title = sources[0].Title
	}

	var output bytes.Buffer
	if err := indexTemplate.Execute(&output, pageData{
		Title:           title,
		Configuration:   string(encoded),
		ScalarURL:       scalarURL,
		ScalarIntegrity: scalarIntegrity,
	}); err != nil {
		return nil, fmt.Errorf("render index page: %w", err)
	}
	return output.Bytes(), nil
}

func setSecurityHeaders(header http.Header) {
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}
