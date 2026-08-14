# NEEDFIX — generator defect-fixing protocol

> **Load together with:** @.contexts/GOGH-GO-GEN.md — the rendering API (`gogh`) the generator is built on.
> Read it before touching any `generate_*.go` file.

## Purpose

Fix defects in the `msgpack-go-gen` generator that are demonstrated by fixture structs in @internal/sample/needfix.go,
prove each fix with round-trip regression tests, and promote fixed structs to @internal/sample/fixed.go.

## File map

| Path                               | Role                                                                                                                     | Edit policy                                                                           |
|------------------------------------|--------------------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------|
| `internal/sample/needfix.go`       | One struct per open defect; comment above each struct describes what the generator emits wrong (or fails to emit at all) | Remove a struct only when it is fixed and moved out. **Never delete the file itself** |
| `internal/sample/fixed.go`         | Home of structs that now work                                                                                            | Add fixed structs here                                                                |
| `internal/sample/generate.go`      | Single `go:generate` line listing which structs get code                                                                 | Keep it in sync: a struct not listed here is silently not generated                   |
| `internal/sample/fixed_test.go`    | Regression tests                                                                                                         | Add one test set per fixed struct                                                     |
| `internal/sample/data.go`          | Stable benchmark/test fixtures (`Data`, `Flat`, `Scalars`, `Request`, `RequestCheck`)                                    | Do not touch                                                                          |
| `internal/sample/data_msgp_gen.go` | tinylib/msgp output, benchmark reference only                                                                            | Do not touch                                                                          |
| `generate_marshaler.go`            | Marshaler emission (fully inlined; `genInlineValue` recurses through nested types)                                       | Fix marshal defects here                                                              |
| `generate_unmarshaler.go`          | Unmarshaler emission (one memoized free function per type; `ensureUnmarshaler`/`drainUnmarshalers`)                      | Fix unmarshal defects here                                                            |
| `generator.go`                     | Shared state: `needsBuffer`, receiver-name cache, `alterFieldCount` AST parsing                                          | Update predicates here                                                                |
| `generator_types.go`               | `fieldCountCorrectMethod` variants (offset vs change form)                                                               | Rarely touched                                                                        |

## Command loop (exact order, every iteration)

```shell
go install .                    # 1. rebuild the binary — go:generate runs the INSTALLED one
go generate ./internal/sample   # 2. regenerate internal/sample/*_gen.go
go vet ./...                    # 3. static check
go test ./internal/sample       # 4. functional tests
```

Skipping step 1 is the classic trap: you then test the previous binary and conclude "my fix had no effect".

## Procedure — one pass per defect struct

Execute the steps in order; do not continue past a failed gate.

### Step 1 — inventory

**Action:** read @internal/sample/needfix.go. **Output:** list of `(struct, defect)` pairs from the comments. **Gate:**
every struct in the file is accounted for. If the file holds only `package sample`, there is nothing to do — stop.

### Step 2 — register

**Action:** ensure each defect struct name appears in the `go:generate` directive of @internal/sample/generate.go.
**Gate:** `grep <Struct> internal/sample/generate.go` matches for every struct.

### Step 3 — reproduce

**Action:** run the command loop. **Gate:** you can observe the defect: generator error, compile error in generated
code, or wrong round-trip result — matching the comment.

### Step 4 — fix the generator

**Action:** modify the generator sources (see file map). Fix the generator, never the generated files. Only change a
fixture struct if its comment says the struct itself is invalid. **Constraints:** keep the generated-code contract below
intact. **Gate:** command loop is fully green for the defect struct.

### Step 5 — promote

**Action:** move the fixed struct (with its `msgpack` tags and any helper methods such as
`alterFieldCount`) from `needfix.go` into `fixed.go`; rerun the command loop so its generated code migrates from
`needfix_gen.go` to `fixed_gen.go`. **Gate:** struct absent from needfix.go, present in fixed.go, both files compile;
needfix.go still exists.

### Step 6 — regression tests

**Action:** add round-trip tests in @internal/sample/data_test.go using
`github.com/vmihailenco/msgpack/v5` as the reference implementation in **both directions**:

```
A -> MarshalMsgpack  -> msgpack.Unmarshal -> A'     must give A' == A
A -> msgpack.Marshal -> UnmarshalMsgpack  -> A'     must give A' == A
```

Preconditions: `UnmarshalMsgpack` is called on a zero-value struct with a fresh
`msgpunsafe.NewSafeBuffer(...)`. Fixture values must exercise the defect (empty strings/slices/maps, negative and
boundary numbers, nested containers) — all-zero fixtures hide wire-format bugs.

**Template** (match this style: plain `testing` + `github.com/sirkon/errors` wrapping +
`deepequal.SideBySide` comparison):

```go
package sample

func TestXRoundTrip(t *testing.T) {
	want := X{ /* fill every field, including nested containers */ }

	data, err := want.MarshalMsgpack(nil)
	if err != nil {
		t.Fatal(errors.Wrap(err, "marshal X with generated implementation"))
	}
	var viaRef X
	if err := msgpack.Unmarshal(data, &viaRef); err != nil {
		t.Fatal(errors.Wrap(err, "unmarshal with reference implementation"))
	}
	deepequal.SideBySide(t, "structure X", want, viaRef)

	ref, err := msgpack.Marshal(&want)
	if err != nil {
		t.Fatal(errors.Wrap(err, "marshal with reference implementation"))
	}
	var viaGen X
	if err := viaGen.UnmarshalMsgpack(ref, msgpunsafe.NewSafeBuffer(128)); err != nil {
		t.Fatal(errors.Wrap(err, "unmarshal with generated implementation"))
	}
	deepequal.SideBySide(t, "structure X", want, viaGen)
}
```

**Gate:** new tests pass and no existing test regressed.

### Step 7 — definition of done

All of the following hold:

- `go build ./... && go vet ./... && go test ./...` green.
- Fixed structs live in `fixed.go` with passing round-trip tests.
- `needfix.go` still exists, holding only remaining open defects.
- No hand edits inside any `*_gen.go`.

## Generated-code contract (MUST hold after every fix)

- Signatures: `func (r *T) MarshalMsgpack(dst []byte) ([]byte, error)` and
  `func (r *T) UnmarshalMsgpack(src []byte, buf *msgpunsafe.SafeBuffer) error`.
- Receiver name is shared between marshaler and unmarshaler (generator caches it).
- Wire name = `msgpack:"..."` tag; empty tag → Go field name; `msgpack:"-"` and unexported fields are skipped.
- `needsBuffer(type)` (contains `string` or `[]byte`, recursively) decides whether a free unmarshal function carries the
  `buf *msgpunsafe.SafeBuffer` parameter. When adding type support, update
  `needsBuffer`, the function emission, and `genUnmarshalCall` **together** — signatures must match at every call site.
- Unmarshal failures surface as panics (`msgpu.ErrorUnknownField`, reader panics) converted to errors by the
  `defer/recover` + `msgpu.HandleError` wrapper in the public method. Free functions never return errors; keep this
  shape.
- Maps: `map[string]T` only (generator errors on other key types). `int`/`uint` marshal as 64-bit.
- Render imports via the custom importer shortcuts — `r.Imports().Msgp()` / `r.Imports().MsgpUnsafe()`
  — and refer to them as `$msgp` / `$msgpu` in format strings. Never hardcode import paths or package qualifiers in
  rendered lines.

## Generator internals cheat sheet

- Marshalers are fully inlined: nested structs/slices/maps expand in place via `genInlineValue`; msgpack map headers are
  emitted by hand (`0x80|n`, `0xDE`, `0xDF`); values via `msgp.Append*`.
- Unmarshalers are the opposite: one private free function per type, memoized in `fnNames`.
  `ensureUnmarshaler` registers the name **before** scheduling the body — this ordering is what makes recursive types
  terminate; keep it. `drainUnmarshalers` emits queued bodies sequentially. Free functions walk
  `(src, lim unsafe.Pointer)` cursor pairs with `msgpunsafe.Take*` readers.
- `main.go` processes structs alphabetically; per output file `pub` (methods) is a gogh `Z()` block placed **before**
  `funcs` (free functions). Write into the right stream when adding output.
- `getFieldCoundOffset` parses `alterFieldCount` bodies from the AST: they must be exactly one
  `return <int literal>` statement.

## MUST NOT

- Delete `internal/sample/needfix.go`.
- Hand-edit generated `*_gen.go` files.
- Touch `internal/sample/data.go` fixtures or `data_msgp_gen.go`.
- Run `go generate` without `go install .` first.
- Translate the existing Russian comments (in generator sources and generated output, e.g.
  `// Поле: name`) as a drive-by change.
