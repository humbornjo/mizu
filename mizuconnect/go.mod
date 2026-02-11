module github.com/humbornjo/mizu/mizuconnect

go 1.25

replace github.com/humbornjo/mizu => ../

require (
	connectrpc.com/connect v1.19.1
	connectrpc.com/grpchealth v1.4.0
	connectrpc.com/grpcreflect v1.3.0
	connectrpc.com/validate v0.6.0
	connectrpc.com/vanguard v0.3.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.8
	github.com/humbornjo/mizu v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.11.1
	golang.org/x/sync v0.19.0
	google.golang.org/genproto/googleapis/api v0.0.0-20260209200024-4cfbd4190f57
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.9-20250912141014-52f32327d4b0.1 // indirect
	buf.build/go/protovalidate v1.0.0 // indirect
	cel.dev/expr v0.24.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/cel-go v0.26.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stoewer/go-strcase v1.3.1 // indirect
	golang.org/x/exp v0.0.0-20250911091902-df9299821621 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260209200024-4cfbd4190f57 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
