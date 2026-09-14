// module is the canonical import path for repository CUE packages.
// It must match go.mod's module path: mizucue renders OpenAPI
// component names from CUE import paths and mizuoai renders them
// from Go package paths — equal module paths make the two converge.
module: "example.com/mizu"
// language pins the CUE language behavior used by schemas and generation.
language: {
	// version is the minimum CUE language version required by this module.
	version: "v0.16.1"
}
