package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	if module, ok := i.moduleCache[resolvedPath]; ok {
		return module, nil
	}
	if i.loadingModule[resolvedPath] {
		return nil, fmt.Errorf("circular import detected for %q", resolvedPath)
	}

	i.loadingModule[resolvedPath] = true
	defer delete(i.loadingModule, resolvedPath)

	source, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read module %q: %w", importPath, err)
	}
	program, err := parser.ParseSource(string(source))
	if err != nil {
		return nil, fmt.Errorf("parse module %q: %w", importPath, err)
	}

	env := NewEnvironment(i.globals)
	env.Define("__file__", resolvedPath)
	env.Define("__dir__", filepath.Dir(resolvedPath))

	signal, err := i.executeBlock(ctx, env, program.Statements, filepath.Dir(resolvedPath))
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
	i.moduleCache[resolvedPath] = module
	return module, nil
}

func resolveModulePath(baseDir, importPath string) (string, error) {
	if strings.TrimSpace(importPath) == "" {
		return "", fmt.Errorf("import path cannot be empty")
	}

	resolved := importPath
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(baseDir, resolved)
	}
	resolved = filepath.Clean(resolved)

	if filepath.Ext(resolved) == "" {
		candidate := resolved + ".vibe"
		if _, err := os.Stat(resolved); err != nil {
			if os.IsNotExist(err) {
				resolved = candidate
			}
		}
	}

	absolute, err := filepath.Abs(resolved)
	if err == nil {
		resolved = absolute
	}
	return resolved, nil
}

func defaultModuleAlias(path string) string {
	base := filepath.Base(path)
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
