package oaisvc

import (
	_ "embed"

	"github.com/humbornjo/mizu/mizucue"
	"github.com/humbornjo/mizu/mizuoai"
)

//go:embed schema.cue
var _SCHEMA_CUE string

var (
	_CUE_SCHEMA           = mizucue.MustCompile(_SCHEMA_CUE)
	_CUE_OPENAPI_DOCUMENT = mizuoai.MustParseOpenAPI(
		mizucue.MustGenerateOpenAPI(_CUE_SCHEMA, "Mizu CUE Example API", "v1", "ExampleV1"),
	)
)
