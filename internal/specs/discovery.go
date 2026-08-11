package specs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Discover finds OpenAPI entry documents beneath directory. Files directly
// beneath directory are at depth zero.
func Discover(ctx context.Context, directory string, maxDepth int) ([]Entry, error) {
	if maxDepth < 0 {
		return nil, errors.New("discovery depth must not be negative")
	}

	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve discovery directory %s: %w", directory, err)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("read discovery directory %s: %w", directory, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("discovery path %s is not a directory", directory)
	}

	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve discovery directory %s: %w", directory, err)
	}
	resolvedRoot = filepath.Clean(resolvedRoot)

	candidates, inGit, err := gitCandidates(ctx, root)
	if err != nil {
		return nil, err
	}
	if !inGit {
		candidates, err = filesystemCandidates(root, maxDepth)
		if err != nil {
			return nil, err
		}
	}

	entries := make([]Entry, 0, len(candidates))
	for _, candidate := range candidates {
		relative, err := relativeWithin(root, candidate)
		if err != nil || pathDepth(relative) > maxDepth || !hasSupportedExtension(candidate) {
			continue
		}
		if _, err := os.Lstat(candidate); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read discovery candidate %s: %w", candidate, err)
		}

		resolvedFile, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return nil, fmt.Errorf("resolve discovery candidate %s: %w", candidate, err)
		}
		resolvedFile = filepath.Clean(resolvedFile)
		if _, err := relativeWithin(resolvedRoot, resolvedFile); err != nil {
			continue
		}
		if !hasSupportedExtension(resolvedFile) {
			continue
		}
		candidateInfo, err := os.Stat(resolvedFile)
		if err != nil {
			return nil, fmt.Errorf("read discovery candidate %s: %w", candidate, err)
		}
		if !candidateInfo.Mode().IsRegular() {
			continue
		}

		metadata, err := inspectOpenAPIDocument(resolvedFile)
		if err != nil {
			return nil, fmt.Errorf("inspect discovery candidate %s: %w", candidate, err)
		}
		if !metadata.isOpenAPI {
			continue
		}
		title := metadata.title
		if title == "" {
			title = filepath.ToSlash(relative)
		}
		entries = append(entries, Entry{
			Path:  candidate,
			Title: title,
		})
	}

	sort.Slice(entries, func(first, second int) bool {
		return filepath.ToSlash(entries[first].Path) < filepath.ToSlash(entries[second].Path)
	})
	if len(entries) == 0 {
		return nil, fmt.Errorf("no OpenAPI specifications found in %s within depth %d", directory, maxDepth)
	}
	return entries, nil
}

func gitCandidates(ctx context.Context, root string) ([]string, bool, error) {
	command := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			if hasGitMetadata(root) {
				return nil, false, errors.New("discover OpenAPI specifications: git executable not found")
			}
			return nil, false, nil
		}
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && !hasGitMetadata(root) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("identify Git worktree for %s: %w", root, err)
	}

	repositoryRoot := filepath.Clean(strings.TrimSpace(string(output)))
	resolvedRepositoryRoot, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return nil, false, fmt.Errorf("resolve Git worktree %s: %w", repositoryRoot, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, false, fmt.Errorf("resolve discovery directory %s: %w", root, err)
	}
	relativeRoot, err := relativeWithin(resolvedRepositoryRoot, resolvedRoot)
	if err != nil {
		return nil, false, fmt.Errorf("discovery directory %s is outside Git worktree %s", root, resolvedRepositoryRoot)
	}

	arguments := []string{"-C", resolvedRepositoryRoot, "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--"}
	if relativeRoot != "." {
		arguments = append(arguments, filepath.ToSlash(relativeRoot))
	}
	command = exec.CommandContext(ctx, "git", arguments...)
	output, err = command.Output()
	if err != nil {
		return nil, false, fmt.Errorf("list non-ignored files beneath %s: %w", root, err)
	}

	parts := bytes.Split(output, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		resolvedCandidate := filepath.Join(resolvedRepositoryRoot, filepath.FromSlash(string(part)))
		relativeCandidate, err := relativeWithin(resolvedRoot, resolvedCandidate)
		if err != nil {
			continue
		}
		paths = append(paths, filepath.Join(root, relativeCandidate))
	}
	return paths, true, nil
}

func filesystemCandidates(root string, maxDepth int) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := pathDepth(relative)
		if entry.IsDir() {
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if depth <= maxDepth {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("search discovery directory %s: %w", root, err)
	}
	return paths, nil
}

type documentMetadata struct {
	isOpenAPI bool
	title     string
}

func inspectOpenAPIDocument(name string) (documentMetadata, error) {
	content, err := os.ReadFile(name)
	if err != nil {
		return documentMetadata{}, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return documentMetadata{}, nil
	}
	if len(document.Content) == 0 {
		return documentMetadata{}, nil
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return documentMetadata{}, nil
	}

	var metadata documentMetadata
	for index := 0; index+1 < len(root.Content); index += 2 {
		key := root.Content[index]
		value := root.Content[index+1]
		if key.Value == "openapi" && value.Kind == yaml.ScalarNode && strings.TrimSpace(value.Value) != "" {
			metadata.isOpenAPI = true
		}
		if key.Value == "info" && value.Kind == yaml.MappingNode {
			metadata.title = mappingString(value, "title")
		}
	}
	return metadata, nil
}

func mappingString(mapping *yaml.Node, name string) string {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		key := mapping.Content[index]
		value := mapping.Content[index+1]
		if key.Value == name && value.Kind == yaml.ScalarNode && value.Tag == "!!str" {
			return strings.TrimSpace(value.Value)
		}
	}
	return ""
}

func pathDepth(relative string) int {
	return strings.Count(filepath.ToSlash(relative), "/")
}

func hasGitMetadata(directory string) bool {
	current := filepath.Clean(directory)
	for {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}
