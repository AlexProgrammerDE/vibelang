package runtime

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"vibelang/internal/ast"
	"vibelang/internal/parser"
)

type loadedModule struct {
	path    string
	exports map[string]any
}

func (i *Interpreter) executeImport(ctx context.Context, env *Environment, moduleDir string, statement *ast.ImportStmt) error {
	module, err := i.loadModule(ctx, moduleDir, statement.Path)
	if err != nil {
		return err
	}

	alias := statement.Alias
	if alias == "" {
		alias = defaultModuleAlias(module.path)
	}
	env.Define(alias, cloneModuleExports(module.exports))
	return nil
}

func (i *Interpreter) executeFromImport(ctx context.Context, env *Environment, moduleDir string, statement *ast.FromImportStmt) error {
	module, err := i.loadModule(ctx, moduleDir, statement.Path)
	if err != nil {
		return err
	}

	for _, name := range statement.Names {
		value, ok := module.exports[name.Name]
		if !ok {
			return fmt.Errorf("module %q does not export %q", statement.Path, name.Name)
		}
		target := name.Name
		if name.Alias != "" {
			target = name.Alias
		}
		env.Define(target, value)
	}
	return nil
}

func (i *Interpreter) loadModule(ctx context.Context, baseDir, importPath string) (*loadedModule, error) {
	resolvedPath, err := resolveModulePath(baseDir, importPath)
	if err != nil {
		return nil, err
	}

	i.mu.RLock()
	if module, ok := i.moduleCache[resolvedPath]; ok {
		i.mu.RUnlock()
		return module, nil
	}
	if i.loadingModule[resolvedPath] {
		i.mu.RUnlock()
		return nil, fmt.Errorf("circular import detected for %q", resolvedPath)
	}
	i.mu.RUnlock()

	i.mu.Lock()
	if module, ok := i.moduleCache[resolvedPath]; ok {
		i.mu.Unlock()
		return module, nil
	}
	if i.loadingModule[resolvedPath] {
		i.mu.Unlock()
		return nil, fmt.Errorf("circular import detected for %q", resolvedPath)
	}
	i.loadingModule[resolvedPath] = true
	i.mu.Unlock()
	defer func() {
		i.mu.Lock()
		delete(i.loadingModule, resolvedPath)
		i.mu.Unlock()
	}()

	source, err := readModuleSource(ctx, resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read module %q: %w", importPath, err)
	}
	program, err := parser.ParseSource(string(source))
	if err != nil {
		return nil, fmt.Errorf("parse module %q: %w", importPath, err)
	}

	moduleDir := moduleBaseDir(resolvedPath)
	env := NewEnvironment(i.globals)
	env.Define("__file__", resolvedPath)
	env.Define("__dir__", moduleDir)

	signal, err := i.executeBlock(ctx, env, program.Statements, moduleDir)
	if err != nil {
		return nil, err
	}
	if signal != signalNone {
		return nil, fmt.Errorf("module %q ended with unexpected control flow", importPath)
	}

	module := &loadedModule{
		path:    resolvedPath,
		exports: env.ExportedValues(),
	}
	i.mu.Lock()
	i.moduleCache[resolvedPath] = module
	i.mu.Unlock()
	return module, nil
}

func resolveModulePath(baseDir, importPath string) (string, error) {
	if strings.TrimSpace(importPath) == "" {
		return "", fmt.Errorf("import path cannot be empty")
	}

	trimmed := strings.TrimSpace(importPath)

	if strings.HasPrefix(trimmed, "github.com/") {
		return resolveGitHubModulePath(trimmed)
	}
	if isRemoteModulePath(trimmed) {
		return ensureRemoteModuleExtension(trimmed)
	}
	if isRemoteModulePath(baseDir) {
		return resolveRemoteModulePath(baseDir, trimmed)
	}

	candidates := make([]string, 0)
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, ".") {
		candidates = append(candidates, normalizedLocalModulePath(baseDir, trimmed))
	} else {
		candidates = append(candidates, normalizedLocalModulePath(baseDir, trimmed))
		for _, root := range moduleSearchRoots() {
			candidates = append(candidates, normalizedLocalModulePath(root, trimmed))
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	ordered := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		ordered = append(ordered, candidate)
	}

	for _, candidate := range ordered {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	if len(ordered) == 0 {
		return "", fmt.Errorf("could not resolve import path %q", importPath)
	}
	return ordered[0], nil
}

func defaultModuleAlias(path string) string {
	base := path
	if isRemoteModulePath(path) {
		parsed, err := url.Parse(path)
		if err == nil {
			base = parsed.Path
		}
	}
	base = filepath.Base(base)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "module"
	}
	return base
}

func cloneModuleExports(exports map[string]any) map[string]any {
	cloned := make(map[string]any, len(exports))
	for name, value := range exports {
		cloned[name] = cloneValue(value)
	}
	return cloned
}

func readModuleSource(ctx context.Context, resolvedPath string) ([]byte, error) {
	if !isRemoteModulePath(resolvedPath) {
		return os.ReadFile(resolvedPath)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, resolvedPath, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("%s returned %s", resolvedPath, response.Status)
	}
	return body, nil
}

func moduleBaseDir(resolvedPath string) string {
	if !isRemoteModulePath(resolvedPath) {
		return filepath.Dir(resolvedPath)
	}
	parsed, err := url.Parse(resolvedPath)
	if err != nil {
		return resolvedPath
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = path.Dir(parsed.Path)
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	return parsed.String()
}

func moduleSearchRoots() []string {
	roots := make([]string, 0)
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable))
	}
	for _, entry := range filepath.SplitList(os.Getenv("VIBE_PATH")) {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		roots = append(roots, entry)
	}
	return roots
}

func normalizedLocalModulePath(baseDir, importPath string) string {
	resolved := importPath
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(baseDir, resolved)
	}
	resolved = filepath.Clean(resolved)
	if filepath.Ext(resolved) == "" {
		resolved += ".vibe"
	}
	absolute, err := filepath.Abs(resolved)
	if err == nil {
		return absolute
	}
	return resolved
}

func isRemoteModulePath(importPath string) bool {
	return strings.HasPrefix(importPath, "http://") || strings.HasPrefix(importPath, "https://")
}

func ensureRemoteModuleExtension(importPath string) (string, error) {
	parsed, err := url.Parse(importPath)
	if err != nil {
		return "", err
	}
	if filepath.Ext(parsed.Path) == "" {
		parsed.Path += ".vibe"
	}
	return parsed.String(), nil
}

func resolveRemoteModulePath(baseDir, importPath string) (string, error) {
	if strings.HasPrefix(importPath, "github.com/") {
		return resolveGitHubModulePath(importPath)
	}
	if isRemoteModulePath(importPath) {
		return ensureRemoteModuleExtension(importPath)
	}

	baseURL, err := url.Parse(baseDir)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(importPath)
	if err != nil {
		return "", err
	}
	return ensureRemoteModuleExtension(baseURL.ResolveReference(ref).String())
}

func resolveGitHubModulePath(importPath string) (string, error) {
	trimmed := strings.TrimPrefix(importPath, "github.com/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return "", fmt.Errorf("github module imports must look like github.com/<owner>/<repo>/<path>@<ref>")
	}

	owner := parts[0]
	repo := parts[1]
	modulePath := strings.Join(parts[2:], "/")
	ref := "main"
	if before, after, ok := strings.Cut(modulePath, "@"); ok {
		modulePath = before
		ref = after
	}
	if modulePath == "" {
		return "", fmt.Errorf("github module path cannot be empty")
	}
	if filepath.Ext(modulePath) == "" {
		modulePath += ".vibe"
	}
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", owner, repo, ref, modulePath), nil
}
