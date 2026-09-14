package mizuoai_test

import (
	"net/http"
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/humbornjo/mizu/mizuoai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type TestOperationPatchInput struct {
	Body struct {
		Name string `json:"name"`
	} `json:"body"`
}

const _OPERATION_PATCH = `{
	"operationId": "publishThing",
	"summary": "patched summary",
	"requestBody": {"content": {"multipart/form-data": {"schema": {"type": "object"}}}},
	"responses": {"409": {"description": "conflict"}}
}`

// The patch carries what reflection cannot infer: it replaces the
// reflected request body wholesale (multipart/form-data over
// application/json) and overlays responses per status code — the
// reflected 200 survives next to the patched 409.
func TestMizuOai_WithOperationPatch(t *testing.T) {
	srv := mizu.NewServer("test")
	err := mizuoai.Initialize(srv, "test")
	require.NoError(t, err)
	mizuoai.Post(srv, "/things", func(_ http.ResponseWriter, rx mizuoai.Rx[TestOperationPatchInput]) (string, error) {
		return "ok", nil
	}, mizuoai.WithOperationPatch([]byte(_OPERATION_PATCH)))

	document := fetchDocument(t, srv)
	assert.Contains(t, document, "operationId: publishThing")
	assert.Contains(t, document, "multipart/form-data")
	assert.NotContains(t, document, "application/json")
	assert.Contains(t, document, "conflict")
	assert.Contains(t, document, "OK")
}

// Options apply in order: fields set by a later option win over the
// patch.
func TestMizuOai_WithOperationPatchOrder(t *testing.T) {
	srv := mizu.NewServer("test")
	err := mizuoai.Initialize(srv, "test")
	require.NoError(t, err)
	mizuoai.Post(srv, "/things", func(_ http.ResponseWriter, rx mizuoai.Rx[TestOperationPatchInput]) (string, error) {
		return "ok", nil
	},
		mizuoai.WithOperationPatch([]byte(_OPERATION_PATCH)),
		mizuoai.WithOperationSummary("late summary"),
	)

	document := fetchDocument(t, srv)
	assert.Contains(t, document, "late summary")
	assert.NotContains(t, document, "patched summary")
}

// A malformed fragment panics at option construction.
func TestMizuOai_WithOperationPatchParsePanics(t *testing.T) {
	assert.Panics(t, func() {
		mizuoai.WithOperationPatch([]byte("{{{"))
	})
}

// A fragment's local refs target the final document's components,
// unknown at option construction: placeholders let the shell build,
// and the rendered document keeps the refs once a document patch
// supplies the components.
func TestMizuOai_WithOperationPatchRefs(t *testing.T) {
	const patch = `{
		"requestBody": {"content": {"multipart/form-data": {"schema": {"$ref": "#/components/schemas/Patched"}}}},
		"responses": {"200": {"description": "ok", "content": {"text/plain": {"schema": {"$ref": "#/components/schemas/Patched"}}}}}
	}`
	srv := mizu.NewServer("test")
	err := mizuoai.Initialize(srv, "test", mizuoai.WithDocumentPatch("test", []byte(`
openapi: 3.0.0
info: {title: lib, version: v1}
paths: {}
components:
  schemas:
    Patched: {type: object, properties: {x: {type: string}}}
`)))
	require.NoError(t, err)
	mizuoai.Post(srv, "/things", func(_ http.ResponseWriter, rx mizuoai.Rx[TestOperationPatchInput]) (string, error) {
		return "ok", nil
	}, mizuoai.WithOperationPatch([]byte(patch)))

	document := fetchDocument(t, srv)
	assert.Contains(t, document, "#/components/schemas/Patched")
}
