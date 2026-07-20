# msgpack-go-gen

Generator for msgpack serialization with the support for manual embedding.

## Installation

```shell
go install github.com/sirkon/msgpack-go-gen
```

## How to use it.

```shell
msgpack-gen-go -p ./internal/dto Data1 Data2:+- Data3:-+
```

Here we generate both marshaler and unmarshaler for `Data1`, marshaler only for `Data2` and
unmarshaler only for `Data3`.

## What it is about.

There can be circumstances with whatever puprpose structures with mandatory fields. Something like

```go
type Request struct {
Mandatory string `msgpack:"mandatory"`
// The rest of fields.
}
```

The rest of fields could have been in their own payload structure of course, like

```go
type Request[T any] struct {
Mandatory string `msgpack:"mandatory"`
Payload   T      `msgpack:"payload"`
}
```

But, this is not always possible – there's a thing called "loads of legacy code", you know.

And in Go, you can't just

```go
type Request[T any] struct {
Mandatory string `msgpack:"mandatory"`
T
}
```

because it is explicitly forbidden. And neither [tinylib/msgp](https://github.com/tinylib/msgp), neither
[vmihailenco/msgpack/vXXX](https://github/vmihailenco/msgpack/v5) support any kind of "inline" in tags
to address this.

## How this thing solves the issue.

This code generation solves this at the marshaling level. All you need is to:

1. Define
   ```go
   func (p *Payload) alterFieldCount() int {
       return 1 // How many additonal fields you want to append.
   }
   ```
2. Generate `MarshalMsgPack` with this utilty.
3. Write a function that appends that mandatory field:
   ```go
   func MarshalPayload(dst []byte, p *Payload, mandatory string) ([]byte, error) {
       dst, err := p.MarshalMsgPack(p)
       if err != nil {
           return err
       }
   
       dst = msgp.AppendString("mandatory") // mgsp = github.com/tinylib/msgp/msgp
       dst = msgp.AppendString(mandatory)
   
       return dst
   }
   ```

## Unmarshaler.

Unlike the marshaler, unmarshaler does not have unique features and basically the same
what you have with `github.com/tinylib/msgp`. Can be a bit faster with proper tuning,
something like 10-25% faster. That said, it is 2nd grade citizen, it is Marshaling
that was the main driver of this generator.

## Benchmarks

| goos  | goarch | cpu                                  | pkg                                              |
|-------|--------|--------------------------------------|--------------------------------------------------|
| linux | amd64  | 12th Gen Intel(R) Core(TM) i7-12700K | github.com/sirkon/msgpack-go-gen/internal/sample |

Testing is done over Data and Flat structures:

```go
type Data struct {
	Name  string `msgpack:"name"`
	Count int    `msgpack:"count"`

	Subs     []Sub `msgpack:"subs"`
	Internal struct {
		Value float32 `msgpack:"value"`
	} `msgpack:"internal"`
	Weights []uint64 `msgpack:"weights"`

	Meta  map[string]Sub  `msgpack:"meta"`
	Flags map[string]bool `msgpack:"flags"`
}

type Sub struct {
	Key    string `msgpack:"key"`
	Active bool   `msgpack:"active"`
}

type Flat struct {
	Name       string `msgpack:"name"`
	Surname    string `msgpack:"surname"`
	Patronymic string `msgpack:"patronymic"`
	City       string `msgpack:"city"`
	Age        int    `msgpack:"age"`
	Weight     int    `msgpack:"weight"`
	Fortune    int    `msgpack:"fortune"`
}
```

Where each pass is a marshal/unmarshal of 65536 structures. So, `Data/marshal` 18248814 ns/op means
278 ns per one Data.

**Run**
```shell
go test  -bench='^BenchmarkAgainst'  -cpu 20 ./internal/sample
```

**Comparison: sirkon vs tinylib/msgp**

Against [tinylib/msgp](https://github.com/tinylib/msgp). Another code generator for msgpack.

| Test           | sirkon         | tinylib/msgp   | Ratio (2nd/1st) |
|----------------|----------------|----------------|-----------------|
| Data/marshal   | 18248814 ns/op | 21469219 ns/op | 1.18x           |
| Data/unmarshal | 33972190 ns/op | 42337165 ns/op | 1.25x           |
| Flat/marshal   | 2176055 ns/op  | 2191606 ns/op  | 1.01x           |
| Flat/unmarshal | 5049333 ns/op  | 7093626 ns/op  | 1.40x           |

**Comparison: sirkon vs vmihailenco/msgpack/v5**

Against reflection-based msgpack parsing [library](https://github.com/vmihailenco/msgpack/v5)

| Test           | sirkon         | vmihailenco     | Ratio (2nd/1st) |
|----------------|----------------|-----------------|-----------------|
| Data/marshal   | 18336712 ns/op | 103941400 ns/op | 5.67x           |
| Data/unmarshal | 34384006 ns/op | 179946069 ns/op | 5.23x           |
| Flat/marshal   | 2176194 ns/op  | 19058167 ns/op  | 8.76x           |
| Flat/unmarshal | 5041637 ns/op  | 27820408 ns/op  | 5.52x           |
