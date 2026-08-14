# AGENTS.md

## What this project is

`msgpack-go-gen` (binary name: `msgpack-go-gen`, module `github.com/sirkon/msgpack-go-gen`) is a Go
**code generator** that emits msgpack `MarshalMsgpack`/`UnmarshalMsgpack` methods for structs, tuned
for Tarantool workloads. It exists because neither `tinylib/msgp` nor `vmihailenco/msgpack` supports
"inline"/embedded struct fields in tags; this tool solves that at the marshaling level via an
`alterFieldCount() int` hook (see README.md for the full motivation).

The root package (`package main`) **is the generator itself**. `internal/sample/` is both the test
fixture and the benchmark harness — it is where generated code is exercised and compared against
`tinylib/msgp`, `vmihailenco/msgpack/v5`, and `encoding/json`.

## Essential commands

```shell
go build ./...                      # build everything
go test ./...                       # all tests (only internal/sample has test files)
go test ./internal/sample           # functional tests
go vet ./...                        # no CI exists; vet is the lint step
```

Regenerating the sample code (the `go:generate` directive in `internal/sample/generate.go` invokes
the **installed binary**, so install/build first):

```shell
go install .                        # puts msgpack-go-gen on PATH
go generate ./internal/sample       # regenerates internal/sample/data_gen.go
```

Benchmarks (each pass marshals/unmarshals 65536 structs; README explains ns/op interpretation):

```shell
go test -bench='^BenchmarkSirkon' -cpu 20 ./internal/sample      # this generator only
go test -bench=. -cpu 20 ./internal/sample                        # vs msgp, vmihailenco, std JSON
```

Bench names are `BenchmarkSirkon`, `BenchmarkTinylibMsgp`, `BenchmarkVMihailenco`, `BenchmarkStdJSON`
(the README's `-bench='^BenchmarkAgainst'` is stale). `data_msgp_gen.go` is committed output of
`tinylib/msgp` (installed via `mise.toml`) used solely for benchmarking — don't regenerate it unless
comparing against a new msgp version.

CLI usage: `msgpack-go-gen -p <pkg-dir> Struct1 Struct2:+- Struct3:-+` — policy suffix `:XY` means
X=marshal, Y=unmarshal (`+`/`-`); no suffix = both. `-p` defaults to `.`. The tool `chdir`s into the
package dir and runs `go list -json` (via `jsonexec`) to resolve the import path.

## Architecture and control flow

`main.go:job` is the whole pipeline:

1. `chdir` into `-p` dir → `go list -json` for the import path.
2. `packages.Load` (full type info + syntax) the package; exactly one package expected.
3. Parse each CLI struct policy, resolve it via `pkg.Types.Scope().Lookup`.
4. Create a `gogh` module (`gogh.New[*importer]`, custom importer in `generator_importer.go`) and
   one file renderer pair per destination file.
5. Struct names are processed **alphabetically** for stable output order.
6. `generator.genMarshaler` / `generator.genUnmarshaler` emit code; `m.Render()` (gofmt) writes files.

**File naming:** output goes next to the source file where the struct is defined: `<source>_gen.go`
(e.g. `data.go` → `data_gen.go`). If two structs live in the same source file they share one
generated file. Note `main.go` builds this with `strings.TrimRight(fileName, ".go")` — this trims a
character *set*, not an extension (works for `data.go`, would mangle names ending in `g`/`o`/`.`).

**Per-file dual renderers:** each destination file has `pub` (public methods) and `funcs` (private
free functions). `pub = r.Z()` is a *lazy* gogh renderer whose block appears **before** `funcs`, so
methods always render above free functions regardless of emission order. When adding output, pick the
right stream consciously.

### Generator internals (`generator*.go`)

- `generator` (in `generator.go`) holds all cross-struct state: `recvNames` (receiver-name cache so
  marshal/unmarshal share one receiver), `fnNames`/`usedNames`/`queue` (package-wide free-function
  registry for unmarshalers), `simpleAllocCache` (`needsBuffer` memo).
- **Marshaler** (`generate_marshaler.go`) is fully *inlined*: nested structs/slices/maps are written
  inline via `genInlineValue`, using `msgp.Append*` functions and hand-computed msgpack map headers
  (`0x80|n`, `0xDE`, `0xDF`). No per-nested-type functions are generated.
- **Unmarshaler** (`generate_unmarshaler.go`) is the opposite: one private free function per type
  (memoized via `ensureUnmarshaler`/`drainUnmarshalers` queue; recursion-safe because the name is
  registered before the body is scheduled). Free fns operate on `unsafe.Pointer` cursor pairs
  `(src, lim)` and use `github.com/sirkon/msgpunsafe` (`Take*` readers). Naming:
  `unmarshal<base>` where base derives from the type (`...Ptr`, `...Slice`, `KeyElemMap`, ...).
- **`needsBuffer(t)`**: recursively true iff the type contains `string` or `[]byte`. It decides
  whether generated free functions take an extra `buf *msgpunsafe.SafeBuffer` parameter. Changing
  type support means touching `needsBuffer`, the emission code, and `genUnmarshalCall` together.
- `emitUnmarshalerStruct` generates a `switch key` with `default: panic(msgpu.ErrorUnknownField)`;
  the public `UnmarshalMsgpack` wraps the call in `defer/recover` + `msgpu.HandleError` — that's how
  errors surface, keep it that way.
- `generator_types.go` encodes the `alterFieldCount` analysis result: offset form (method takes no
  args, body must be exactly `return <int literal>` — parsed from AST in `getFieldCoundOffset`) or
  change form (method takes one `int` param, field count computed at runtime).

## Generated-code contract (what output must look like)

```go
func (r *T) MarshalMsgpack(dst []byte) ([]byte, error)
func (r *T) UnmarshalMsgpack(src []byte, buf *msgpunsafe.SafeBuffer) error
```

- Header: `// Code generated by msgpack-go-gen version <ver>. DO NOT EDIT.` (via `gogh.Autogen`).
- Receiver name is reused from an existing method of the type if one exists, else first letter
  lowercased.
- Field wire names come from `msgpack:"..."` tags; missing tag → Go field name; tag `-` → skipped;
  unexported fields skipped.
- Maps are **`map[string]T` only** (both directions error out otherwise).
- Marshal: `int`/`uint` are widened to 64-bit (`AppendInt64`/`AppendUint64`).
- Unmarshal strings/[]byte are copied into the caller's `SafeBuffer` (zero-copy from input buffer is
  intentional; input must outlive the struct).

## Working on generator bugs — the NEEDFIX workflow

`.contexts/NEEDFIX.md` defines the repo's fix loop; follow it:

1. Broken/unsupported constructs live as structs in `internal/sample/needfix.go`, each with a
   comment describing the failure.
2. Ensure the struct is listed in the `go:generate` line of `internal/sample/generate.go`.
3. Regenerate, fix the generator until output compiles and round-trips.
4. Move the fixed struct to `internal/sample/fixed.go` (never delete `needfix.go`).
5. Add round-trip regression tests using `vmihailenco/msgpack/v5` as the reference in **both**
   directions: generated-marshal→reference-unmarshal and reference-marshal→generated-unmarshal,
   comparing against the original value with `deepequal.SideBySide`, unmarshaling into zero values.

`internal/sample/data.go` holds the stable benchmark/test fixtures (`Data`, `Flat`, `Scalars`,
`Request`/`RequestCheck` — the latter pair demonstrates `alterFieldCount` + manual extra-field
appending); don't move those.

## Conventions and gotchas

- **gogh is the rendering DSL** — before modifying any `generate_*.go` code, read
  `.contexts/GOGH-GO-GEN.md`; it is the authoritative API reference (format strings `$0`/`$name`,
  `Uniq`, `@ident`, `Let`, `Scope()` vs `Z()`, `Q()` for quoted literals, the `T()`/`Void()`
  side-effect renderers, etc.). Non-obvious essentials: imports are file-global; `Render()` at the
  end is what writes anything; `Ref` names must be unique per file.
- The custom `importer` (`generator_importer.go`) adds typed shortcuts `Msgp()` and `MsgpUnsafe()`;
  generated code refers to them as `$msgp` and `$msgpu` — use these shortcuts, never hardcode
  import paths in rendered lines.
- `goRenderer` is a type alias (`type goRenderer = gogh.GoRenderer[*importer]`) — keep using it.
- **Comments and some identifiers in the generator source are in Russian** (e.g. `// Поле: name`
  in generated output, comments in `generator.go`/`generate_marshaler.go`). Match surrounding style;
  don't translate existing comments as a drive-by change.
- Errors use `github.com/sirkon/errors` (`Wrap`/`Wrapf`/`Newf`) and `github.com/sirkon/message` for
  logging — not stdlib `errors`/`fmt` for generator errors. `g.errorf` reports with source position.
- Tests use plain `testing` + `deepequal.SideBySide(t, "structure X", want, got)` — no testify in
  `internal/sample` despite it being an indirect dep.
- Commit messages follow `fix: <lowercase imperative>` (see `git log`); there is no CI config.
- `go.mod` declares `go 1.26`; generator code uses range-over-func (`for method := range
  desc.Methods()`), `maps`/`slices` stdlib packages.
