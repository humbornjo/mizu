package mizuoai_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuoai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestPatchedModel struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type TestEmptyInput struct{}

// canonicalName mirrors internal.CanonicalTypeName for the test
// package: "github.com.humbornjo.mizu.mizuoai_test.TestPatchedModel".
const _PATCHED_MODEL_NAME = "github.com.humbornjo.mizu.mizuoai_test.TestPatchedModel"

const _PATCH_LIB = `
openapi: 3.0.0
info: {title: lib, version: v1}
paths: {}
components:
  schemas:
    github.com.humbornjo.mizu.mizuoai_test.TestPatchedModel:
      type: object
      properties:
        name: {type: string}
        email: {type: string, pattern: "^[^@]+@[^@]+$"}
      required: [name, email]
`

const _PATCH_SHARED_OBJECT = `
openapi: 3.0.0
info: {title: a, version: v1}
paths: {}
components:
  schemas:
    example.com.test.Shared:
      type: object
      properties:
        name: {type: string}
`

const _PATCH_SHARED_STRING = `
openapi: 3.0.0
info: {title: b, version: v1}
paths: {}
components:
  schemas:
    example.com.test.Shared:
      type: string
`

func fetchDocument(t *testing.T, srv *mizu.Server) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	return recorder.Body.String()
}

// A patch schema carrying the reflected type's canonical name
// shadows the reflected schema at render: the CUE constraints win.
func TestMizuOai_WithDocumentPatchShadowsReflected(t *testing.T) {
	srv := mizu.NewServer("test")
	err := mizuoai.Initialize(srv, "test",
		mizuoai.WithDocumentPatch("example.com/test/lib", []byte(_PATCH_LIB)),
	)
	require.NoError(t, err)
	mizuoai.Get(srv, "/model", func(_ http.ResponseWriter, rx mizuoai.Rx[TestEmptyInput]) (TestPatchedModel, error) {
		return TestPatchedModel{}, nil
	})

	document := fetchDocument(t, srv)
	assert.Contains(t, document, "$ref: '#/components/schemas/"+_PATCHED_MODEL_NAME+"'")
	// The reflected schema has no pattern; only the patch does.
	assert.Contains(t, document, `^[^@]+@[^@]+$`)
}

// Two patches contributing the same schema with identical content
// dedupe silently.
func TestMizuOai_WithDocumentPatchDedupesIdentical(t *testing.T) {
	srv := mizu.NewServer("test")
	err := mizuoai.Initialize(srv, "test",
		mizuoai.WithDocumentPatch("example.com/test/a", []byte(_PATCH_SHARED_OBJECT)),
		mizuoai.WithDocumentPatch("example.com/test/b", []byte(_PATCH_SHARED_OBJECT)),
	)
	require.NoError(t, err)
	assert.Contains(t, fetchDocument(t, srv), "example.com.test.Shared")
}

// A name repeated across patches dedupes by name — canonical
// naming makes the name the schema's identity; the first patch
// wins, later ones are dropped silently.
func TestMizuOai_WithDocumentPatchFirstWins(t *testing.T) {
	srv := mizu.NewServer("test")
	err := mizuoai.Initialize(srv, "test",
		mizuoai.WithDocumentPatch("example.com/test/a", []byte(_PATCH_SHARED_OBJECT)),
		mizuoai.WithDocumentPatch("example.com/test/b", []byte(_PATCH_SHARED_STRING)),
	)
	require.NoError(t, err)
	document := fetchDocument(t, srv)
	assert.Contains(t, document, "example.com.test.Shared")
	assert.Contains(t, document, "type: object")
}

// An unparseable patch panics at option construction, naming the
// provenance.
func TestMizuOai_WithDocumentPatchParsePanics(t *testing.T) {
	assert.Panics(t, func() {
		mizuoai.WithDocumentPatch("example.com/test/bad", []byte("{{{"))
	})
}

// A patch without components is a no-op.
func TestMizuOai_WithDocumentPatchEmpty(t *testing.T) {
	srv := mizu.NewServer("test")
	err := mizuoai.Initialize(srv, "test",
		mizuoai.WithDocumentPatch("example.com/test/empty", []byte(`
openapi: 3.0.0
info: {title: empty, version: v1}
paths: {}
`)),
	)
	require.NoError(t, err)
	assert.NotContains(t, fetchDocument(t, srv), "example.com.test")
}
