package config

// Config is the app-level configuration contract. The Go type is
// generated from this definition (see cue_types_gen.go), koanf
// decodes local.yaml into it, and mizucue validates the result —
// one source of truth for shape, defaults, and constraints.
#Config: {
	// env is the deployment environment.
	env!: string & ("local" | "dev" | "test" | "prod") | *"local"

	// port is the HTTP listen address.
	port!: string & =~"^:[0-9]{2,5}$" | *":18080"

	// level is the minimum slog level name.
	level!: string & ("DEBUG" | "INFO" | "WARN" | "ERROR") | *"INFO"
}
