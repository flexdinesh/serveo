package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flexdinesh/serveo/internal/specs"
)

func TestAppRendersPinnedScalarConfiguration(t *testing.T) {
	root := t.TempDir()
	entry := writeFile(t, root, "api/openapi.yaml", "openapi: 3.1.1\ninfo:\n  title: '<unsafe>'\n")
	collection, err := specs.Prepare([]string{entry}, root)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Config{
		Specs:          collection,
		ServerOverride: "https://api.example.com",
		AllowedHost:    "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	response := request(t, app, http.MethodGet, "/", "example.com")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	body := response.Body.String()
	for _, expected := range []string{
		scalarURL,
		`crossorigin="anonymous"`,
		`href="/assets/app.css"`,
		`src="/assets/app.js"`,
		`\u003cunsafe\u003e`,
		`<title>&lt;unsafe&gt;</title>`,
		`&#34;disabled&#34;:true`,
		`&#34;telemetry&#34;:false`,
		`&#34;showDeveloperTools&#34;:&#34;never&#34;`,
		`&#34;withDefaultFonts&#34;:false`,
		`&#34;persistAuth&#34;:false`,
		`&#34;url&#34;:&#34;https://api.example.com&#34;`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("page does not contain %q\n%s", expected, body)
		}
	}
	if !strings.Contains(html.UnescapeString(body), scalarIntegrity) {
		t.Errorf("page does not contain integrity metadata %q", scalarIntegrity)
	}
	if strings.Contains(body, "<unsafe>") {
		t.Fatal("unsafe title was rendered as markup")
	}
	if strings.Contains(body, "customCss") {
		t.Fatal("page contains custom Scalar CSS")
	}
}

func TestAppServesSpecsAndReferencedDocuments(t *testing.T) {
	root := t.TempDir()
	entry := writeFile(t, root, "apis/first spec.yaml", "openapi: 3.1.1\n")
	reference := writeFile(t, root, "components/item.json", `{"type":"object"}`)
	collection, err := specs.Prepare([]string{entry}, root)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Config{Specs: collection, AllowedHost: "example.com"})
	if err != nil {
		t.Fatal(err)
	}

	yamlResponse := request(t, app, http.MethodGet, "/spec/apis/first%20spec.yaml", "example.com")
	if yamlResponse.Code != http.StatusOK || yamlResponse.Body.String() != "openapi: 3.1.1\n" {
		t.Fatalf("YAML response = %d %q", yamlResponse.Code, yamlResponse.Body.String())
	}
	if got := yamlResponse.Header().Get("Content-Type"); got != "application/yaml; charset=utf-8" {
		t.Fatalf("YAML Content-Type = %q", got)
	}

	jsonResponse := request(t, app, http.MethodGet, "/spec/components/item.json", "example.com")
	if jsonResponse.Code != http.StatusOK || jsonResponse.Body.String() != `{"type":"object"}` {
		t.Fatalf("JSON response = %d %q", jsonResponse.Code, jsonResponse.Body.String())
	}
	if got := jsonResponse.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("JSON Content-Type = %q", got)
	}

	if err := os.WriteFile(reference, []byte(`{"type":"string"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := request(t, app, http.MethodGet, "/spec/components/item.json", "example.com")
	if updated.Body.String() != `{"type":"string"}` {
		t.Fatalf("updated body = %q", updated.Body.String())
	}
}

func TestAppAppliesSecurityAndHostRestrictions(t *testing.T) {
	app := newTestApp(t)
	response := request(t, app, http.MethodGet, "/", "example.com")
	for _, header := range []string{
		"Cache-Control",
		"Content-Security-Policy",
		"Referrer-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
	} {
		if response.Header().Get(header) == "" {
			t.Errorf("%s is empty", header)
		}
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("CSP does not prevent framing")
	}

	misdirected := request(t, app, http.MethodGet, "/", "attacker.example")
	if misdirected.Code != http.StatusMisdirectedRequest {
		t.Fatalf("misdirected status = %d", misdirected.Code)
	}
	if misdirected.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing from rejected request")
	}
}

func TestAppHandlesRoutesMethodsAndHead(t *testing.T) {
	app := newTestApp(t)

	stylesheet := request(t, app, http.MethodGet, "/assets/app.css", "example.com")
	if stylesheet.Code != http.StatusOK || !strings.Contains(stylesheet.Body.String(), "--scalar-sidebar-width") {
		t.Fatalf("stylesheet response = %d %q", stylesheet.Code, stylesheet.Body.String())
	}
	if got := stylesheet.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Fatalf("stylesheet Content-Type = %q", got)
	}

	javaScript := request(t, app, http.MethodGet, "/assets/app.js", "example.com")
	if javaScript.Code != http.StatusOK ||
		!strings.Contains(javaScript.Body.String(), "Scalar.createApiReference") ||
		!strings.Contains(javaScript.Body.String(), "window.sessionStorage") {
		t.Fatalf("JavaScript response = %d %q", javaScript.Code, javaScript.Body.String())
	}
	if !strings.HasPrefix(javaScript.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("JavaScript Content-Type = %q", javaScript.Header().Get("Content-Type"))
	}

	head := request(t, app, http.MethodHead, "/", "example.com")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD response = %d %q", head.Code, head.Body.String())
	}

	post := request(t, app, http.MethodPost, "/", "example.com")
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", post.Code)
	}
	if !strings.Contains(post.Header().Get("Allow"), http.MethodGet) {
		t.Fatalf("Allow = %q", post.Header().Get("Allow"))
	}

	unknown := request(t, app, http.MethodGet, "/unknown", "example.com")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown status = %d", unknown.Code)
	}

	missing := request(t, app, http.MethodGet, "/spec/missing.yaml", "example.com")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", missing.Code)
	}
	unsupported := request(t, app, http.MethodGet, "/spec/secret.txt", "example.com")
	if unsupported.Code != http.StatusNotFound {
		t.Fatalf("unsupported status = %d", unsupported.Code)
	}
}

func TestNewValidatesConfiguration(t *testing.T) {
	if _, err := New(Config{AllowedHost: "example.com"}); err == nil {
		t.Fatal("New with nil specs returned nil error")
	}
	root := t.TempDir()
	entry := writeFile(t, root, "openapi.yaml", "openapi: 3.1.1\n")
	collection, err := specs.Prepare([]string{entry}, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Specs: collection}); err == nil {
		t.Fatal("New with empty allowed host returned nil error")
	}
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	entry := writeFile(t, root, "openapi.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "secret.txt", "secret")
	collection, err := specs.Prepare([]string{entry}, root)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Config{Specs: collection, AllowedHost: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func request(t *testing.T, handler http.Handler, method, target, host string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func writeFile(t *testing.T, root, name, content string) string {
	t.Helper()
	target := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}
