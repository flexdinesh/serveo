package specs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestPreparePreservesOrderedSourcesAndFindsCommonRoot(t *testing.T) {
	root := t.TempDir()
	first := writeFile(t, root, "apis/first spec.yaml", "openapi: 3.1.1\n")
	second := writeFile(t, root, "schemas/second.json", `{"openapi":"3.1.1"}`)

	collection, err := Prepare([]string{first, second}, "")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	if got := collection.RootPath(); got != root {
		t.Fatalf("RootPath() = %q, want %q", got, root)
	}
	want := []Source{
		{Title: "first spec.yaml", Slug: "spec-1", URL: "/spec/apis/first%20spec.yaml"},
		{Title: "second.json", Slug: "spec-2", URL: "/spec/schemas/second.json"},
	}
	if got := collection.Sources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Sources() = %#v, want %#v", got, want)
	}
}

func TestPrepareUsesExplicitRoot(t *testing.T) {
	root := t.TempDir()
	entry := writeFile(t, root, "nested/openapi.yml", "openapi: 3.1.1\n")

	collection, err := Prepare([]string{entry}, root)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if got := collection.RootPath(); got != root {
		t.Fatalf("RootPath() = %q, want %q", got, root)
	}
	if got := collection.Sources()[0].URL; got != "/spec/nested/openapi.yml" {
		t.Fatalf("source URL = %q", got)
	}
}

func TestPrepareAcceptsSupportedExtensionsCaseInsensitively(t *testing.T) {
	for _, extension := range []string{".yaml", ".YAML", ".yml", ".YML", ".json", ".JSON"} {
		t.Run(strings.TrimPrefix(extension, "."), func(t *testing.T) {
			root := t.TempDir()
			entry := writeFile(t, root, "openapi"+extension, "{}")
			if _, err := Prepare([]string{entry}, ""); err != nil {
				t.Fatalf("Prepare() error = %v", err)
			}
		})
	}
}

func TestSourcesReturnsDefensiveCopy(t *testing.T) {
	root := t.TempDir()
	entry := writeFile(t, root, "openapi.yaml", "openapi: 3.1.1\n")
	collection, err := Prepare([]string{entry}, "")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	sources := collection.Sources()
	sources[0].Title = "changed"
	sources = append(sources, Source{Title: "added"})

	got := collection.Sources()
	if len(got) != 1 || got[0].Title != "openapi.yaml" {
		t.Fatalf("Sources() after caller mutation = %#v", got)
	}
}

func TestPrepareRejectsInvalidEntries(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "directory.yaml")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	tests := []struct {
		name  string
		files []string
		want  error
	}{
		{name: "no files", want: nil},
		{name: "missing", files: []string{filepath.Join(root, "missing.yaml")}, want: os.ErrNotExist},
		{name: "unsupported extension", files: []string{writeFile(t, root, "openapi.txt", "{}")}, want: ErrUnsupportedType},
		{name: "directory", files: []string{directory}, want: ErrNotRegular},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Prepare(test.files, "")
			if err == nil {
				t.Fatal("Prepare() error = nil")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("Prepare() error = %v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}
}

func TestPrepareRejectsInvalidExplicitRoots(t *testing.T) {
	base := t.TempDir()
	entry := writeFile(t, base, "openapi.yaml", "{}")
	rootFile := writeFile(t, base, "root.json", "{}")

	for _, test := range []struct {
		name string
		root string
	}{
		{name: "missing", root: filepath.Join(base, "missing")},
		{name: "file", root: rootFile},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Prepare([]string{entry}, test.root); err == nil {
				t.Fatal("Prepare() error = nil")
			}
		})
	}
}

func TestPrepareRejectsEntryOutsideExplicitRoot(t *testing.T) {
	root := t.TempDir()
	outside := writeFile(t, t.TempDir(), "openapi.yaml", "{}")

	_, err := Prepare([]string{outside}, root)
	if !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Prepare() error = %v, want ErrOutsideRoot", err)
	}
}

func TestResolveReturnsSupportedRegularFiles(t *testing.T) {
	root := t.TempDir()
	entry := writeFile(t, root, "apis/open api.YAML", "openapi: 3.1.1\n")
	reference := writeFile(t, root, "components/item.JSON", `{"type":"object"}`)
	collection, err := Prepare([]string{entry}, root)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	for _, test := range []struct {
		relative string
		want     string
	}{
		{relative: "apis/open api.YAML", want: resolvedPath(t, entry)},
		{relative: "components/item.JSON", want: resolvedPath(t, reference)},
		{relative: "components/./item.JSON", want: resolvedPath(t, reference)},
	} {
		t.Run(test.relative, func(t *testing.T) {
			got, err := collection.Resolve(test.relative)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Resolve() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveRejectsInvalidAndTraversalPaths(t *testing.T) {
	root := t.TempDir()
	entry := writeFile(t, root, "openapi.yaml", "{}")
	collection, err := Prepare([]string{entry}, root)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	for _, relative := range []string{
		"",
		".",
		"/openapi.yaml",
		filepath.Join(root, "openapi.yaml"),
		"../openapi.yaml",
		"nested/../../openapi.yaml",
		"nested/../openapi.yaml",
	} {
		t.Run(relative, func(t *testing.T) {
			if _, err := collection.Resolve(relative); !errors.Is(err, ErrInvalidPath) {
				t.Fatalf("Resolve(%q) error = %v, want ErrInvalidPath", relative, err)
			}
		})
	}
}

func TestResolveRejectsMissingUnsupportedAndNonRegularPaths(t *testing.T) {
	root := t.TempDir()
	entry := writeFile(t, root, "openapi.yaml", "{}")
	writeFile(t, root, "secret.txt", "secret")
	directory := filepath.Join(root, "directory.yaml")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	collection, err := Prepare([]string{entry}, root)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	if _, err := collection.Resolve("missing.yaml"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Resolve(missing) error = %v, want os.ErrNotExist", err)
	}
	if _, err := collection.Resolve("secret.txt"); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("Resolve(txt) error = %v, want ErrUnsupportedType", err)
	}
	if _, err := collection.Resolve("directory.yaml"); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("Resolve(directory) error = %v, want ErrNotRegular", err)
	}
}

func TestPrepareRejectsEntrySymlinkEscape(t *testing.T) {
	requireSymlinks(t)
	root := t.TempDir()
	outside := writeFile(t, t.TempDir(), "outside.yaml", "{}")
	link := filepath.Join(root, "openapi.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := Prepare([]string{link}, root)
	if !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Prepare() error = %v, want ErrOutsideRoot", err)
	}
}

func TestResolveRejectsSymlinkEscapes(t *testing.T) {
	requireSymlinks(t)
	root := t.TempDir()
	entry := writeFile(t, root, "openapi.yaml", "{}")
	outsideRoot := t.TempDir()
	outside := writeFile(t, outsideRoot, "outside.yaml", "secret")
	fileLink := filepath.Join(root, "file-link.yaml")
	if err := os.Symlink(outside, fileLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	directoryLink := filepath.Join(root, "directory-link")
	if err := os.Symlink(outsideRoot, directoryLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	collection, err := Prepare([]string{entry}, root)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	for _, relative := range []string{"file-link.yaml", "directory-link/outside.yaml"} {
		if _, err := collection.Resolve(relative); !errors.Is(err, ErrOutsideRoot) {
			t.Errorf("Resolve(%q) error = %v, want ErrOutsideRoot", relative, err)
		}
	}
}

func TestResolveAllowsContainedSymlink(t *testing.T) {
	requireSymlinks(t)
	root := t.TempDir()
	entry := writeFile(t, root, "openapi.yaml", "{}")
	target := writeFile(t, root, "schemas/target.yml", "type: object\n")
	link := filepath.Join(root, "linked.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	collection, err := Prepare([]string{entry}, root)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	got, err := collection.Resolve("linked.yml")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := resolvedPath(t, target)
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestResolveRejectsSymlinkToUnsupportedType(t *testing.T) {
	requireSymlinks(t)
	root := t.TempDir()
	entry := writeFile(t, root, "openapi.yaml", "{}")
	target := writeFile(t, root, "secret.txt", "secret")
	link := filepath.Join(root, "alias.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	collection, err := Prepare([]string{entry}, root)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	if _, err := collection.Resolve("alias.yaml"); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("Resolve() error = %v, want ErrUnsupportedType", err)
	}
}

func TestNilCollectionAccessors(t *testing.T) {
	var collection *Collection
	if got := collection.RootPath(); got != "" {
		t.Fatalf("RootPath() = %q", got)
	}
	if got := collection.Sources(); got != nil {
		t.Fatalf("Sources() = %#v", got)
	}
	if _, err := collection.Resolve("openapi.yaml"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Resolve() error = %v, want ErrInvalidPath", err)
	}
}

func writeFile(t *testing.T, root, name, contents string) string {
	t.Helper()
	target := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return target
}

func requireSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available on Windows")
	}
}

func resolvedPath(t *testing.T, name string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", name, err)
	}
	return resolved
}
