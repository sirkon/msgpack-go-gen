# msgpack-go-gen
Generator for msgpack serialization with the support for manual embedding.

## Installation

```shell
go install github.com/sirkon/msgpack-go-gen
```

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
