// Command mizucuegen bakes every CUE package's generated OpenAPI
// document into an openapi.yaml next to the package's CUE files.
//
//	mizucuegen [dir]
//
// It locates the enclosing CUE module by walking up from dir
// (default ".") until a cue.mod/module.cue, loads every package of
// that module with mizucue, and writes one openapi.yaml per package
// directory.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"go.yaml.in/yaml/v3"

	"github.com/humbornjo/mizu/mizucue"
)

func main() {
	dir := "."
	if len(os.Args) > 2 {
		fatal(fmt.Errorf("usage: mizucuegen [dir]"))
	}
	if len(os.Args) == 2 {
		dir = os.Args[1]
	}
	if err := run(dir); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mizucuegen:", err)
	os.Exit(1)
}

func run(dir string) error {
	root, err := findModuleRoot(dir)
	if err != nil {
		return err
	}
	modulePath, err := readModulePath(root)
	if err != nil {
		return err
	}
	module, err := mizucue.LoadModule(os.DirFS(root))
	if err != nil {
		return err
	}
	documents, err := module.OpenAPIs(nil)
	if err != nil {
		return err
	}
	for importPath, data := range documents {
		rel, err := packageDir(modulePath, importPath)
		if err != nil {
			return err
		}
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("import path %s escapes the module root", importPath)
		}
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("decode generated document for %s: %w", importPath, err)
		}
		yamlData, err := yaml.Marshal(document)
		if err != nil {
			return fmt.Errorf("encode yaml for %s: %w", importPath, err)
		}
		out := filepath.Join(root, filepath.FromSlash(rel), "openapi.yaml")
		// #nosec G703 -- rel is proven local by the filepath.IsLocal
		// guard above; root is the module the user points the tool at.
		if err := os.WriteFile(out, yamlData, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", out, err)
		}
		fmt.Println("wrote", out)
	}
	return nil
}

// findModuleRoot walks up from dir until a directory holding
// cue.mod/module.cue — the CUE module root.
func findModuleRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	for {
		if info, err := os.Stat(filepath.Join(abs, "cue.mod", "module.cue")); err == nil && !info.IsDir() {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no cue.mod/module.cue found from %s upward", dir)
		}
		abs = parent
	}
}

// readModulePath reads the declared module path from
// cue.mod/module.cue.
func readModulePath(root string) (string, error) {
	// #nosec G304 G703 -- reading the module.cue of the
	// user-designated module is the tool's purpose.
	data, err := os.ReadFile(filepath.Join(root, "cue.mod", "module.cue"))
	if err != nil {
		return "", fmt.Errorf("read module.cue: %w", err)
	}
	value := cuecontext.New().CompileBytes(data)
	modulePath, err := value.LookupPath(cue.MakePath(cue.Str("module"))).String()
	if err != nil {
		return "", fmt.Errorf("read module path from module.cue: %w", err)
	}
	return modulePath, nil
}

// packageDir maps a package's import path back to its directory
// relative to the module root; a package at the root maps to ".".
func packageDir(modulePath, importPath string) (string, error) {
	if importPath == modulePath {
		return ".", nil
	}
	rel, ok := strings.CutPrefix(importPath, modulePath+"/")
	if !ok {
		return "", fmt.Errorf("import path %s is not under module %s", importPath, modulePath)
	}
	return rel, nil
}
