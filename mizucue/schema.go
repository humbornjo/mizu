package mizucue

import (
	"fmt"
	"reflect"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/encoding/openapi"
)

// LoadSchema compiles an inline CUE schema.
func LoadSchema(raw string) (Schema, error) {
	context := cuecontext.New()
	value := context.CompileString(raw)
	if err := value.Err(); err != nil {
		return nil, fmt.Errorf("compile CUE schema: %w", err)
	}
	return &schema{context: context, value: value}, nil
}

// MustLoadSchema compiles an inline CUE schema or panics on failure.
func MustLoadSchema(raw string) Schema {
	schema, err := LoadSchema(raw)
	if err != nil {
		panic(err)
	}
	return schema
}

// Schema is a compiled CUE schema: it validates generated Go models
// and, as sugar, renders its own raw OpenAPI document. Assembling
// documents across packages is the consumer's job.
type Schema interface {
	// Validate checks a Go model against the CUE definition named
	// after the model's concrete type. Nil values, missing
	// definitions, and non-concrete results are rejected.
	Validate(value any) error

	// OpenAPI generates the schema's raw CUE OpenAPI document as
	// JSON bytes. It is sugar over openapi.Generate: config passes
	// through untouched, the output is verbatim, and every call
	// regenerates — nothing is cached.
	OpenAPI(config *openapi.Config) ([]byte, error)

	// MustOpenAPI generates the schema's OpenAPI document or panics
	// on failure.
	MustOpenAPI(config *openapi.Config) []byte

	// Operation renders the schema's operation fragment named after
	// t's concrete type as JSON bytes: type PublishSkillOperation is
	// served by the member "#PublishSkillOperation", mirroring
	// Validate's naming. The fragment carries what reflection cannot
	// infer per operation — request body media types, response
	// tables — and merges over the reflected operation in mizuoai
	// (WithOperationPatch). The member must be concrete. Given:
	//
	//	#PublishSkillOperation: {
	//		operationId: "publishSkill"
	//		requestBody: content: "multipart/form-data": schema: type: "object"
	//	} @go(-)
	//
	// Operation(reflect.TypeFor[PublishSkillOperation]()) returns:
	//
	//	{
	//		"operationId": "publishSkill",
	//		"requestBody": {
	//			"content": {
	//				"multipart/form-data": {
	//					"schema": { "type": "object" }
	//				}
	//			}
	//		}
	//	}
	Operation(t reflect.Type) ([]byte, error)

	// MustOperation renders the type's operation fragment or panics
	// on failure.
	MustOperation(t reflect.Type) []byte
}

var _ Schema = (*schema)(nil)

type schema struct {
	context *cue.Context
	value   cue.Value
}

func (s *schema) Validate(value any) error {
	typ := reflect.TypeOf(value)
	if typ == nil {
		return fmt.Errorf("validate model: nil value")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	definition := s.value.LookupPath(cue.ParsePath("#" + typ.Name()))
	if err := definition.Err(); err != nil {
		return fmt.Errorf("lookup model schema %s: %w", typ.Name(), err)
	}
	unified := definition.Unify(s.context.Encode(value))
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return fmt.Errorf("validate %s: %w", typ.Name(), err)
	}
	return nil
}

func (s *schema) OpenAPI(config *openapi.Config) ([]byte, error) {
	file, err := openapi.Generate(s.value, config)
	if err != nil {
		return nil, err
	}
	generated := s.context.BuildFile(file)
	if err := generated.Err(); err != nil {
		return nil, err
	}
	return generated.MarshalJSON()
}

func (s *schema) MustOpenAPI(config *openapi.Config) []byte {
	data, err := s.OpenAPI(config)
	if err != nil {
		panic(err)
	}
	return data
}

func (s *schema) Operation(t reflect.Type) ([]byte, error) {
	if t == nil {
		return nil, fmt.Errorf("lookup operation fragment: nil type")
	}
	named := t
	for named.Kind() == reflect.Pointer {
		named = named.Elem()
	}
	definition := s.value.LookupPath(cue.ParsePath("#" + named.Name()))
	if err := definition.Err(); err != nil {
		return nil, fmt.Errorf("lookup operation fragment %s: %w", named.Name(), err)
	}
	if err := definition.Validate(cue.Concrete(true)); err != nil {
		return nil, fmt.Errorf("validate operation fragment %s: %w", named.Name(), err)
	}
	return definition.MarshalJSON()
}

func (s *schema) MustOperation(t reflect.Type) []byte {
	data, err := s.Operation(t)
	if err != nil {
		panic(err)
	}
	return data
}
