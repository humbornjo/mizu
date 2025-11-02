module mizu.example

go 1.24.4

replace github.com/humbornjo/mizu => ../

replace github.com/humbornjo/mizu/mizudi => ../mizudi/

replace github.com/humbornjo/mizu/mizuoai => ../mizuoai/

replace github.com/humbornjo/mizu/mizuotel => ../mizuotel/

replace github.com/humbornjo/mizu/mizuconnect => ../mizuconnect/

require (
	connectrpc.com/connect v1.19.1
	github.com/humbornjo/mizu v0.0.0-20251028144032-c8e95fbda45c
	github.com/humbornjo/mizu/mizuconnect v0.0.0-20251028142926-633981b80da7
	github.com/humbornjo/mizu/mizudi v0.0.0-20251028144032-c8e95fbda45c
	github.com/humbornjo/mizu/mizuoai v0.0.0-20251028142926-633981b80da7
	github.com/humbornjo/mizu/mizuotel v0.0.0-20251028144032-c8e95fbda45c
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.62.0
	google.golang.org/genproto/googleapis/api v0.0.0-20251022142026-3a174f9686a8
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
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/getkin/kin-openapi v0.133.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/cel-go v0.26.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/knadh/koanf/parsers/yaml v1.1.0 // indirect
	github.com/knadh/koanf/providers/env v1.1.0 // indirect
	github.com/knadh/koanf/providers/file v1.2.0 // indirect
	github.com/knadh/koanf/v2 v2.3.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oasdiff/yaml v0.0.0-20250309154309-f31be36b4037 // indirect
	github.com/oasdiff/yaml3 v0.0.0-20250309153720-d2182401db90 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.2 // indirect
	github.com/prometheus/procfs v0.19.1 // indirect
	github.com/samber/do/v2 v2.0.0 // indirect
	github.com/samber/go-type-to-string v1.8.0 // indirect
	github.com/stoewer/go-strcase v1.3.1 // indirect
	github.com/woodsbury/decimal128 v1.3.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/sdk v1.38.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20250911091902-df9299821621 // indirect
	golang.org/x/sys v0.37.0 // indirect
	golang.org/x/text v0.30.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251014184007-4626949a642f // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
