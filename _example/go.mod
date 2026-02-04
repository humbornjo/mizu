module mizu.example

go 1.25

replace github.com/humbornjo/mizu => ../

replace github.com/humbornjo/mizu/mizuconnect => ../mizuconnect

replace github.com/humbornjo/mizu/mizudi => ../mizudi

replace github.com/humbornjo/mizu/mizumw => ../mizumw

replace github.com/humbornjo/mizu/mizuoai => ../mizuoai

replace github.com/humbornjo/mizu/mizuotel => ../mizuotel

require (
	connectrpc.com/connect v1.19.1
	github.com/humbornjo/mizu v0.0.0-20251107130138-05723b1570b6
	github.com/humbornjo/mizu/mizuconnect v0.0.0-20251107130138-05723b1570b6
	github.com/humbornjo/mizu/mizudi v0.0.0-20251107130138-05723b1570b6
	github.com/humbornjo/mizu/mizumw v0.0.0-20251107130138-05723b1570b6
	github.com/humbornjo/mizu/mizuoai v0.0.0-20251107130138-05723b1570b6
	github.com/humbornjo/mizu/mizuotel v0.0.0-20251107130138-05723b1570b6
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0
	google.golang.org/genproto/googleapis/api v0.0.0-20251103181224-f26f9409b101
	google.golang.org/protobuf v1.36.10
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.9-20250912141014-52f32327d4b0.1 // indirect
	buf.build/go/protovalidate v1.0.0 // indirect
	cel.dev/expr v0.24.0 // indirect
	connectrpc.com/grpchealth v1.4.0 // indirect
	connectrpc.com/grpcreflect v1.3.0 // indirect
	connectrpc.com/validate v0.6.0 // indirect
	connectrpc.com/vanguard v0.3.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/cel-go v0.26.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/knadh/koanf/parsers/yaml v1.1.0 // indirect
	github.com/knadh/koanf/providers/env v1.1.0 // indirect
	github.com/knadh/koanf/providers/file v1.2.0 // indirect
	github.com/knadh/koanf/v2 v2.3.0 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pb33f/jsonpath v0.1.2 // indirect
	github.com/pb33f/libopenapi v0.28.2 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.0 // indirect
	github.com/samber/do/v2 v2.0.0 // indirect
	github.com/samber/go-type-to-string v1.8.0 // indirect
	github.com/stoewer/go-strcase v1.3.1 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/sdk v1.38.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	go.yaml.in/yaml/v3 v3.0.3 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.2 // indirect
	golang.org/x/exp v0.0.0-20250911091902-df9299821621 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251029180050-ab9386a59fda // indirect
)
