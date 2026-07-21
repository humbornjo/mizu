# Code Style

Use a pragmatic, package-oriented Go style. Favor readable, direct implementations over speculative abstractions.

## Naming

- Use concise conventional local names when their meaning is clear, such as `ctx`, `cfg`, `req`, `resp`, and `err`.
- Keep method receivers short and consistent for a type.
- Use PascalCase for exported identifiers.
- Treat initialisms as ordinary CamelCase segments when another segment follows (`HttpClient`, `RpcMessage`), but keep a
  final initialism uppercase (`PresignURL`). Use `Id`, not `ID`, as the deliberate exception (`ProjectId`).
- Use uppercase snake case for constants, environment variables, wire values, and package globals, optionally with a
  leading underscore (for example, `SERVICE_NAME` and `_SYSTEM_PROMPT`). This established repository convention takes
  precedence over idiomatic Go mixed caps.
- Use `NewX` for constructors and `FromX` for adapters or conversions.
- Prefer locally clear names over long identifiers that repeat package context.

## Implementation

- Keep control flow straight-line and readable from top to bottom. Handle failures with early returns and perform
  initialization and side effects in an explicit order.
- Do not extract a helper merely to shorten a function. Keep single-parent logic inline or in a local closure unless it
  implements a boundary, is recursive or concurrent, or forms a substantial independently testable unit.
- Prefer concrete code over wrappers and abstraction layers. Add an abstraction only when multiple callers genuinely need
  it or an established boundary already exists.
- Do not add pass-through wrappers that provide no validation, translation, policy, or domain behavior.
- Do not introduce a production interface solely for mocking one concrete dependency. Put a narrow mock interface in the
  test file when a focused test needs one.
- Put interfaces at real external or implementation boundaries and add compile-time interface checks where useful.
- Pass `context.Context` consistently through I/O and external calls.
- Return errors from reusable packages rather than panicking.
- Preserve the repository's existing package organization and dependency direction.

## Tests

- Name tests `Test<PackageNameInCamelCase>_<Target>`. Omit an external test package's `_test` suffix from the package
  portion.
- Keep tests function-oriented and clear.
- Use table-driven tests when one target has multiple cases.
- Keep environment-dependent integration checks out of `*_test.go`; use scripts that capture and inspect command output
  instead.
- Do not change production structure solely to make tests easier to mock.

## Formatting and Generated Code

- Format Go code with `gofmt`.
- Format linter suppression comments as `// nolint: <linter>`, with spaces after `//` and before the linter name.
- Follow existing file and package conventions before introducing a new pattern.
- Do not manually edit generated files; update their source definition and regenerate them.
