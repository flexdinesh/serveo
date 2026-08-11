package specs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverFindsOpenAPIDocumentsWithinDepth(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "z.json", `{"openapi":"3.1.1"}`)
	writeFile(t, root, "a/openapi.YAML", "openapi: 3.0.3\n")
	writeFile(t, root, "a/b/openapi.yml", "openapi: 3.1.0\n")
	writeFile(t, root, "a/b/c/openapi.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "a/b/c/d/openapi.yaml", "openapi: 3.1.1\n")

	entries, err := Discover(context.Background(), root, 3)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	wantTitles := []string{
		"a/b/c/openapi.yaml",
		"a/b/openapi.yml",
		"a/openapi.YAML",
		"z.json",
	}
	if got := entryTitles(entries); !reflect.DeepEqual(got, wantTitles) {
		t.Fatalf("Discover() titles = %#v, want %#v", got, wantTitles)
	}
	for index, entry := range entries {
		if !filepath.IsAbs(entry.Path) {
			t.Errorf("entry %d path = %q, want absolute path", index, entry.Path)
		}
	}
}

func TestDiscoverDepthZeroIncludesOnlyDirectFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "openapi.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "nested/openapi.yaml", "openapi: 3.1.1\n")

	entries, err := Discover(context.Background(), root, 0)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := entryTitles(entries), []string{"openapi.yaml"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover() titles = %#v, want %#v", got, want)
	}
}

func TestDiscoverUsesInfoTitleAndPreservesPathOrder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a/openapi.yaml", "openapi: 3.1.1\ninfo:\n  title: Zulu API\n")
	writeFile(t, root, "b/openapi.yaml", "openapi: 3.1.1\ninfo:\n  title: Alpha API\n")

	entries, err := Discover(context.Background(), root, 1)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := entryTitles(entries), []string{"Zulu API", "Alpha API"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover() titles = %#v, want path-ordered %#v", got, want)
	}
}

func TestDiscoverRequiresRootOpenAPIField(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "valid.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "empty.yaml", "openapi: ''\n")
	writeFile(t, root, "swagger.yaml", "swagger: '2.0'\n")
	writeFile(t, root, "schema.yaml", "type: object\n")
	writeFile(t, root, "nested-field.yaml", "metadata:\n  openapi: 3.1.1\n")
	writeFile(t, root, "sequence.yaml", "- openapi: 3.1.1\n")
	writeFile(t, root, "malformed.yaml", "openapi: [\n")
	writeFile(t, root, "unsupported.txt", "openapi: 3.1.1\n")

	entries, err := Discover(context.Background(), root, 0)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := entryTitles(entries), []string{"valid.yaml"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover() titles = %#v, want %#v", got, want)
	}
}

func TestDiscoverHonorsStandardGitIgnores(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--quiet")

	writeFile(t, root, ".gitignore", "ignored-root.yaml\nnested/ignored-nested.yaml\ntracked.yaml\n")
	writeFile(t, root, ".git/info/exclude", "info-excluded.yaml\n")
	globalExcludes := writeFile(t, root, "config/global-excludes", "global-excluded.yaml\n")
	git(t, root, "config", "core.excludesFile", globalExcludes)

	writeFile(t, root, "visible.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "ignored-root.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "nested/ignored-nested.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "info-excluded.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "global-excluded.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "tracked.yaml", "openapi: 3.1.1\n")
	git(t, root, "add", "--force", "tracked.yaml")

	entries, err := Discover(context.Background(), root, 3)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	want := []string{"tracked.yaml", "visible.yaml"}
	if got := entryTitles(entries); !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover() titles = %#v, want %#v", got, want)
	}
}

func TestDiscoverHonorsNestedGitignore(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--quiet")
	writeFile(t, root, "services/.gitignore", "ignored.yaml\n")
	writeFile(t, root, "services/ignored.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "services/visible.yaml", "openapi: 3.1.1\n")

	entries, err := Discover(context.Background(), root, 3)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := entryTitles(entries), []string{"services/visible.yaml"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover() titles = %#v, want %#v", got, want)
	}
}

func TestDiscoverSkipsDeletedTrackedFiles(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--quiet")
	deleted := writeFile(t, root, "deleted.yaml", "openapi: 3.1.1\n")
	writeFile(t, root, "visible.yaml", "openapi: 3.1.1\n")
	git(t, root, "add", "deleted.yaml")
	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}

	entries, err := Discover(context.Background(), root, 3)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got, want := entryTitles(entries), []string{"visible.yaml"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Discover() titles = %#v, want %#v", got, want)
	}
}

func TestDiscoverReportsInvalidInputsAndEmptyResults(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "schema.yaml", "type: object\n")

	if _, err := Discover(context.Background(), root, -1); err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("Discover(negative depth) error = %v", err)
	}
	if _, err := Discover(context.Background(), filepath.Join(root, "missing"), 3); err == nil {
		t.Fatal("Discover(missing directory) error = nil")
	}
	fileRoot := t.TempDir()
	file := writeFile(t, fileRoot, "file.yaml", "openapi: 3.1.1\n")
	if _, err := Discover(context.Background(), file, 3); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("Discover(file) error = %v", err)
	}
	if _, err := Discover(context.Background(), root, 3); err == nil || !strings.Contains(err.Error(), "no OpenAPI specifications found") {
		t.Fatalf("Discover(empty) error = %v", err)
	}
}

func TestPrepareEntriesUsesSuppliedTitles(t *testing.T) {
	root := t.TempDir()
	entry := writeFile(t, root, "services/users/openapi.yaml", "openapi: 3.1.1\n")
	collection, err := PrepareEntries([]Entry{{Path: entry, Title: "services/users/openapi.yaml"}}, root)
	if err != nil {
		t.Fatalf("PrepareEntries() error = %v", err)
	}
	if got := collection.Sources()[0].Title; got != "services/users/openapi.yaml" {
		t.Fatalf("source title = %q", got)
	}
}

func entryTitles(entries []Entry) []string {
	titles := make([]string, len(entries))
	for index, entry := range entries {
		titles[index] = entry.Title
	}
	return titles
}

func git(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}
