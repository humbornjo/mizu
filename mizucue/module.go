//go:generate go run ./cmd/mizucuegen ./internal/testdata

package mizucue

import (
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/openapi"
)

// Module is every CUE package of one CUE module held as loader
// instances, from which validation and OpenAPI views are built.
type Module interface {
	// Extract builds the schema of the first instance whose package
	// name is pkgName. Package names are not unique across a module's
	// directories; selection follows loader order.
	Extract(pkgName string) (Schema, error)

	// MustExtract builds the schema of the named package or panics on
	// failure.
	MustExtract(pkgName string) Schema

	// OpenAPIs generates the raw CUE OpenAPI document of every package
	// and returns them as an import-path-ordered sequence of JSON
	// bytes. Component names are qualified k8s-style with the
	// definition's source package import path — the same shape as
	// mizuoai's CanonicalTypeName — so same-named definitions from
	// different packages coexist in one assembled document and
	// references carry their provenance. A config NameFunc replaces
	// the default naming; every other config field passes to the
	// generator untouched.
	//
	// Each package's import path travels as the sequence key — the
	// declared module path and package dir without the loader's
	// canonical major-version suffix — and is the provenance a
	// consumer passes along with the bytes when assembling the
	// documents (e.g. mizuoai's PatchOpenAPI option).
	OpenAPIs(config *openapi.Config) (iter.Seq2[string, []byte], error)

	// MustOpenAPIs generates every package's OpenAPI document or
	// panics on failure.
	MustOpenAPIs(config *openapi.Config) iter.Seq2[string, []byte]

	// Instances returns the module's CUE loader instances in loader
	// order, exposing the loader's own metadata — import path,
	// package name, files, dependencies — to consumers that need
	// more than the schema and OpenAPI views.
	Instances() iter.Seq[*build.Instance]
}

// LoadModule loads every CUE package of the module in fsys with the
// CUE loader. fsys is the module's filesystem: cue.mod/module.cue at
// the root and one directory per package, imports resolving through
// the declared module path.
//
// The loader's recursive wildcard cannot walk a virtual filesystem,
// so fsys is mirrored into an overlay on a scratch host directory;
// package discovery, module metadata, and CUE file selection stay
// with the loader.
func LoadModule(fsys fs.FS) (Module, error) {
	root, err := os.MkdirTemp("", "mizucue-*")
	if err != nil {
		return nil, fmt.Errorf("load CUE module: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()

	overlay := map[string]load.Source{}
	err = fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		overlay[filepath.Join(root, filepath.FromSlash(name))] = load.FromBytes(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load CUE module: %w", err)
	}

	instances := load.Instances([]string{"./..."}, &load.Config{Dir: root, Overlay: overlay})
	if len(instances) == 0 {
		return nil, fmt.Errorf("load CUE module: no packages found")
	}
	for _, instance := range instances {
		if err := instance.Err; err != nil {
			return nil, fmt.Errorf("load CUE package %s: %w", instance.ImportPath, err)
		}
	}
	return &module{instances: instances}, nil
}

var _ Module = (*module)(nil)

type module struct {
	instances []*build.Instance
}

func (m *module) Extract(pkgName string) (Schema, error) {
	for _, instance := range m.instances {
		if instance.PkgName != pkgName {
			continue
		}
		context := cuecontext.New()
		value := context.BuildInstance(instance)
		if err := value.Err(); err != nil {
			return nil, fmt.Errorf("extract CUE package %s: %w", pkgName, err)
		}
		return &schema{context: context, value: value}, nil
	}
	return nil, fmt.Errorf("extract CUE package %s: package not found", pkgName)
}

func (m *module) MustExtract(pkgName string) Schema {
	schema, err := m.Extract(pkgName)
	if err != nil {
		panic(err)
	}
	return schema
}

func (m *module) OpenAPIs(config *openapi.Config) (iter.Seq2[string, []byte], error) {
	type document struct {
		key  string
		data []byte
	}
	documents := make([]document, 0, len(m.instances))
	for _, instance := range m.instances {
		// The key is the declared module path plus the package's relative
		// dir: the loader's canonical "@version" suffix and any
		// ":qualifier" after it are module-level noise.
		key, _, _ := strings.Cut(instance.ImportPath, "@")
		data, err := func() ([]byte, error) {
			context := cuecontext.New()
			value := context.BuildInstance(instance)
			if err := value.Err(); err != nil {
				return nil, err
			}
			// Copy the caller's config: the default NameFunc closes
			// over the instance under generation, and the caller's
			// config must never be mutated.
			instanceConfig := openapi.Config{}
			if config != nil {
				instanceConfig = *config
			}
			if instanceConfig.NameFunc == nil {
				instanceConfig.NameFunc = m.QualifiedNameFunc(instance)
			}
			file, err := openapi.Generate(value, &instanceConfig)
			if err != nil {
				return nil, err
			}
			generated := context.BuildFile(file)
			if err := generated.Err(); err != nil {
				return nil, err
			}
			return generated.MarshalJSON()
		}()
		if err != nil {
			return nil, fmt.Errorf("generate OpenAPI for CUE package %s: %w", key, err)
		}
		documents = append(documents, document{key, data})
	}
	return func(yield func(string, []byte) bool) {
		for _, document := range documents {
			if !yield(document.key, document.data) {
				return
			}
		}
	}, nil
}

func (m *module) MustOpenAPIs(config *openapi.Config) iter.Seq2[string, []byte] {
	documents, err := m.OpenAPIs(config)
	if err != nil {
		panic(err)
	}
	return documents
}

func (m *module) Instances() iter.Seq[*build.Instance] {
	return slices.Values(m.instances)
}

// QualifiedNameFunc returns an openapi NameFunc naming components
// k8s-style — the definition's source package import path dotted
// with the definition name, aligned with mizuoai's CanonicalTypeName:
// "example.com.mizucue.test.app.TestModel". Import path provenance
// makes same-named definitions from different packages coexist in one
// document instead of silently overwriting each other.
//
// The generator calls the func with the instance value holding the
// definition; for a cross-package reference that is the source
// package's instance, so the name follows the definition's origin,
// not the package under generation.
func (m *module) QualifiedNameFunc(current *build.Instance) func(cue.Value, cue.Path) string {
	// selectorLabel mirrors the CUE generator's default label
	// extraction: definition labels lose their "#", string labels
	// unquote, pattern constraints render as "*".
	selectorLabel := func(sel cue.Selector) string {
		if sel.Type().ConstraintType() == cue.PatternConstraint {
			return "*"
		}
		switch sel.LabelType() {
		case cue.DefinitionLabel:
			return sel.String()[1:]
		case cue.StringLabel:
			return sel.Unquoted()
		default:
			return sel.String()
		}
	}
	// canonicalName sanitizes a dotted type name into a legal OpenAPI
	// component key matching ^[a-zA-Z0-9.\-_]+$: "/" joins collapse to
	// ".", any other disallowed run collapses to "_". Same algorithm as
	// mizuoai's CanonicalTypeName.
	canonicalName := func(full string) string {
		full = strings.ReplaceAll(full, "/", ".")
		var b strings.Builder
		disallowed := false
		for _, r := range full {
			allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
				r == '.' || r == '-' || r == '_'
			if !allowed {
				disallowed = true
				continue
			}
			if disallowed {
				b.WriteByte('_')
				disallowed = false
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	return func(val cue.Value, path cue.Path) string {
		source := current
		if def := val.LookupPath(path); def.Exists() {
			if filename := def.Pos().Filename(); filename != "" {
				for _, instance := range m.instances {
					if filepath.Dir(filename) == instance.Dir {
						source = instance
						break
					}
				}
			}
		}
		importPath, _, _ := strings.Cut(source.ImportPath, "@")

		labels := make([]string, 0, len(path.Selectors()))
		for _, sel := range path.Selectors() {
			labels = append(labels, selectorLabel(sel))
		}
		return canonicalName(importPath + "." + strings.Join(labels, "."))
	}
}
