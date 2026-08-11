// Package specs prepares local OpenAPI documents and safely resolves files
// beneath their serving root.
package specs

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrInvalidPath indicates that a requested path is empty, absolute, or
	// contains a parent-directory component.
	ErrInvalidPath = errors.New("invalid spec path")

	// ErrOutsideRoot indicates that a file is, or resolves through symlinks,
	// outside the collection's serving root.
	ErrOutsideRoot = errors.New("spec file is outside root")

	// ErrUnsupportedType indicates that a file does not have a supported spec
	// extension. Only YAML, YML, and JSON files are supported.
	ErrUnsupportedType = errors.New("unsupported spec file type")

	// ErrNotRegular indicates that a resolved path is not a regular file.
	ErrNotRegular = errors.New("spec path is not a regular file")
)

// Source describes one entry document in the order it was supplied to
// Prepare.
type Source struct {
	Title string
	Slug  string
	URL   string
}

// Entry describes an OpenAPI entry document and the title shown for it in
// the document picker.
type Entry struct {
	Path  string
	Title string
}

// Collection is a prepared set of entry documents and the root from which
// they and their local references may be served.
type Collection struct {
	root         string
	resolvedRoot string
	sources      []Source
}

// Prepare validates entry documents and prepares their common or explicitly
// selected serving root.
func Prepare(files []string, rootFlag string) (*Collection, error) {
	entries := make([]Entry, len(files))
	for index, name := range files {
		entries[index] = Entry{Path: name, Title: filepath.Base(name)}
	}
	return prepareEntries(entries, rootFlag, true)
}

// PrepareEntries validates titled entry documents and prepares their common
// or explicitly selected serving root.
func PrepareEntries(entries []Entry, rootFlag string) (*Collection, error) {
	return prepareEntries(entries, rootFlag, false)
}

func prepareEntries(entries []Entry, rootFlag string, inferTitles bool) (*Collection, error) {
	files := make([]string, len(entries))
	for index, entry := range entries {
		files[index] = entry.Path
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("at least one spec file is required")
	}

	absoluteFiles := make([]string, 0, len(files))
	for _, name := range files {
		if !hasSupportedExtension(name) {
			return nil, fmt.Errorf("%w: %s must be a YAML or JSON file", ErrUnsupportedType, name)
		}

		absolute, err := filepath.Abs(name)
		if err != nil {
			return nil, fmt.Errorf("resolve spec file %s: %w", name, err)
		}
		absolute = filepath.Clean(absolute)

		info, err := os.Stat(absolute)
		if err != nil {
			return nil, fmt.Errorf("read spec file %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s", ErrNotRegular, name)
		}

		absoluteFiles = append(absoluteFiles, absolute)
	}

	root, err := prepareRoot(absoluteFiles, rootFlag)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root %s: %w", root, err)
	}
	resolvedRoot = filepath.Clean(resolvedRoot)

	sources := make([]Source, 0, len(absoluteFiles))
	relativePaths := make([]string, 0, len(absoluteFiles))
	for index, absolute := range absoluteFiles {
		relative, err := relativeWithin(root, absolute)
		if err != nil {
			return nil, fmt.Errorf("%w: spec file %s is outside root %s", ErrOutsideRoot, files[index], root)
		}

		resolvedFile, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, fmt.Errorf("resolve spec file %s: %w", files[index], err)
		}
		if _, err := relativeWithin(resolvedRoot, resolvedFile); err != nil {
			return nil, fmt.Errorf("%w: spec file %s resolves outside root %s", ErrOutsideRoot, files[index], root)
		}
		if !hasSupportedExtension(resolvedFile) {
			return nil, fmt.Errorf("%w: spec file %s resolves to %s", ErrUnsupportedType, files[index], resolvedFile)
		}

		title := entries[index].Title
		if inferTitles {
			metadata, err := inspectOpenAPIDocument(resolvedFile)
			if err != nil {
				return nil, fmt.Errorf("inspect spec file %s: %w", files[index], err)
			}
			if metadata.title != "" {
				title = metadata.title
			}
		}

		sources = append(sources, Source{
			Title: title,
			Slug:  fmt.Sprintf("spec-%d", index+1),
			URL:   "/spec/" + escapePath(relative),
		})
		relativePaths = append(relativePaths, filepath.ToSlash(relative))
	}
	disambiguateSourceTitles(sources, relativePaths)

	return &Collection{
		root:         root,
		resolvedRoot: resolvedRoot,
		sources:      sources,
	}, nil
}

func disambiguateSourceTitles(sources []Source, relativePaths []string) {
	counts := make(map[string]int, len(sources))
	for _, source := range sources {
		counts[source.Title]++
	}

	used := make(map[string]int, len(sources))
	for index := range sources {
		candidate := sources[index].Title
		if counts[candidate] > 1 {
			candidate = fmt.Sprintf("%s — %s", candidate, relativePaths[index])
		}

		used[candidate]++
		if used[candidate] > 1 {
			sources[index].Title = fmt.Sprintf("%s (%d)", candidate, used[candidate])
			continue
		}
		sources[index].Title = candidate
	}
}

// RootPath returns the absolute, cleaned path selected as the serving root.
// The returned path retains the spelling of an explicitly supplied symlink;
// containment checks use its resolved target internally.
func (collection *Collection) RootPath() string {
	if collection == nil {
		return ""
	}
	return collection.root
}

// Sources returns a copy of the ordered entry-document metadata.
func (collection *Collection) Sources() []Source {
	if collection == nil {
		return nil
	}
	return append([]Source(nil), collection.sources...)
}

// Resolve returns the canonical absolute path for a decoded, root-relative
// slash path. It accepts only regular YAML, YML, or JSON files contained by
// the collection's resolved root.
func (collection *Collection) Resolve(relative string) (string, error) {
	if collection == nil {
		return "", fmt.Errorf("%w: nil collection", ErrInvalidPath)
	}

	localPath, err := cleanRelativePath(relative)
	if err != nil {
		return "", err
	}
	if !hasSupportedExtension(localPath) {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedType, relative)
	}

	target := filepath.Join(collection.root, localPath)
	if _, err := relativeWithin(collection.root, target); err != nil {
		return "", fmt.Errorf("%w: %s", ErrOutsideRoot, relative)
	}

	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve spec path %s: %w", relative, err)
	}
	resolvedTarget = filepath.Clean(resolvedTarget)
	if _, err := relativeWithin(collection.resolvedRoot, resolvedTarget); err != nil {
		return "", fmt.Errorf("%w: %s resolves outside root", ErrOutsideRoot, relative)
	}
	if !hasSupportedExtension(resolvedTarget) {
		return "", fmt.Errorf("%w: %s resolves to %s", ErrUnsupportedType, relative, resolvedTarget)
	}

	info, err := os.Stat(resolvedTarget)
	if err != nil {
		return "", fmt.Errorf("read spec path %s: %w", relative, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s", ErrNotRegular, relative)
	}

	return resolvedTarget, nil
}

func prepareRoot(files []string, rootFlag string) (string, error) {
	if rootFlag == "" {
		return commonRoot(files), nil
	}

	root, err := filepath.Abs(rootFlag)
	if err != nil {
		return "", fmt.Errorf("resolve root %s: %w", rootFlag, err)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("read root %s: %w", rootFlag, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root %s is not a directory", rootFlag)
	}
	return root, nil
}

func commonRoot(files []string) string {
	root := filepath.Dir(files[0])
	for _, file := range files[1:] {
		for {
			if _, err := relativeWithin(root, file); err == nil {
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

func cleanRelativePath(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.HasPrefix(relative, "/") {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, relative)
	}

	// Check before cleaning so even a traversal that would happen to land back
	// inside the root is rejected.
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: %q contains parent traversal", ErrInvalidPath, relative)
		}
	}

	cleaned := filepath.Clean(filepath.FromSlash(relative))
	if cleaned == "." || cleaned == ".." || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, relative)
	}
	return cleaned, nil
}

func relativeWithin(root, target string) (string, error) {
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", ErrOutsideRoot
	}
	return relative, nil
}

func hasSupportedExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

func escapePath(relative string) string {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
