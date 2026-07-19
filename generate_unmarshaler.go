package main

import (
	"go/token"
	"go/types"
	"reflect"
	"strconv"

	"github.com/sirkon/errors"
	"github.com/sirkon/gogh"
)

// basicUnmarshalInfo describes how to unmarshal a given basic kind.
type basicUnmarshalInfo struct {
	base   string // capitalized name used to build the function identifier
	goType string // Go type returned by the msgp reader
	takeFn string // name of the msgp.Read*Bytes function
}

var basicUnmarshalTable = map[types.BasicKind]basicUnmarshalInfo{
	types.String:  {base: "String", goType: "string", takeFn: "TakeString"},
	types.Bool:    {base: "Bool", goType: "bool", takeFn: "TakeBool"},
	types.Int:     {base: "Int", goType: "int", takeFn: "TakeInt"},
	types.Int8:    {base: "Int8", goType: "int8", takeFn: "TakeInt8"},
	types.Int16:   {base: "Int16", goType: "int16", takeFn: "TakeInt16"},
	types.Int32:   {base: "Int32", goType: "int32", takeFn: "TakeInt32"},
	types.Int64:   {base: "Int64", goType: "int64", takeFn: "TakeInt64"},
	types.Uint:    {base: "Uint", goType: "uint", takeFn: "TakeUint"},
	types.Uint8:   {base: "Uint8", goType: "uint8", takeFn: "TakeUint8"},
	types.Uint16:  {base: "Uint16", goType: "uint16", takeFn: "TakeUint16"},
	types.Uint32:  {base: "Uint32", goType: "uint32", takeFn: "TakeUint32"},
	types.Uint64:  {base: "Uint64", goType: "uint64", takeFn: "TakeUint64"},
	types.Float32: {base: "Float32", goType: "float32", takeFn: "TakeFloat32"},
	types.Float64: {base: "Float64", goType: "float64", takeFn: "TakeFloat64"},
}

func (g *generator) genUnmarshaler(r *goRenderer, name string, desc *types.Named) error {
	// Register and emit the free function responsible for the type. The public
	// method below merely delegates to it, which also satisfies the requirement
	// that a type used both standalone and nested reuses the same free function.
	fnName := g.ensureUnmarshaler(desc)
	if err := g.drainUnmarshalers(); err != nil {
		return errors.Wrap(err, "generate free unmarshal functions for "+desc.String())
	}

	m := r.Scope()
	m.Imports().MsgpUnsafe().Ref("msgpu")
	m.Imports().Add("unsafe").Ref("unsafe")
	m.N()
	m.Let("recv", m.Uniq(g.receiver(name, desc)))
	m.L(`func ($recv *$0) UnmarshalMsgpack(src []byte, buf *$msgpu.SafeBuffer) (err error) {`, desc)
	m.L(`    defer func() {`)
	m.L(`        if r := recover(); r != nil {`)
	m.L(`            $msgpu.HandleError(r, &err)`)
	m.L(`        }`)
	m.L(`    }()`)
	m.N()
	m.L(`    ptrSrc := $unsafe.Pointer(unsafe.SliceData(src))`)
	if g.needsBuffer(desc) {
		m.L(`    _ = $0($recv, ptrSrc, len(src), buf)`, fnName)
	} else {
		m.L(`    _ = $0($recv, ptrSrc, len(src))`, fnName)
	}
	m.L(`    return nil`)
	m.L(`}`)

	return nil
}

// ensureUnmarshaler returns the name of the free unmarshal function for typ,
// registering it (and scheduling its body for emission) on first encounter.
func (g *generator) ensureUnmarshaler(typ types.Type) string {
	key := typ.String()
	if fnName, ok := g.fnNames[key]; ok {
		return fnName
	}

	fnName := g.pub.Uniq(gogh.Private("unmarshal", g.unmarshalerBaseName(typ)))
	// Register the name before scheduling the body so that recursive types do
	// not loop forever.
	g.fnNames[key] = fnName
	g.queue = append(g.queue, typ)

	return fnName
}

// drainUnmarshalers emits the bodies of every scheduled free function. Emission
// is sequential so function bodies never interleave, even though emitting one
// function may schedule new ones.
func (g *generator) drainUnmarshalers() error {
	for len(g.queue) > 0 {
		typ := g.queue[0]
		g.queue = g.queue[1:]

		if err := g.emitUnmarshaler(typ); err != nil {
			return err
		}
	}

	return nil
}

func (g *generator) uniqueUnmarshalerName(base string) string {
	candidate := gogh.Private("unmarshal", base)
	name := candidate
	for i := 1; ; i++ {
		if _, ok := g.usedNames[name]; !ok {
			g.usedNames[name] = struct{}{}
			return name
		}
		name = candidate + strconv.Itoa(i)
	}
}

func (g *generator) unmarshalerBaseName(typ types.Type) string {
	switch t := typ.(type) {
	case *types.Named:
		return t.Obj().Name()
	case *types.Pointer:
		return g.unmarshalerBaseName(t.Elem()) + "Ptr"
	case *types.Slice:
		return g.unmarshalerBaseName(t.Elem()) + "Slice"
	case *types.Map:
		return g.unmarshalerBaseName(t.Key()) + g.unmarshalerBaseName(t.Elem()) + "Map"
	case *types.Basic:
		if info, ok := basicUnmarshalTable[t.Kind()]; ok {
			return info.base
		}
		return "Basic"
	case *types.Struct:
		return "Struct"
	default:
		return "Value"
	}
}

// isUnmarshalValueForm reports whether the free function for typ returns the
// value (scalars and pointers) as opposed to taking a destination pointer
// (containers and structs).
func isUnmarshalValueForm(typ types.Type) bool {
	if _, ok := typ.(*types.Pointer); ok {
		return true
	}
	_, ok := typ.Underlying().(*types.Basic)
	return ok
}

// genUnmarshalCall writes the single-statement unmarshaling call for a value
// stored at lhs. Containers and structs use the pointer form, scalars and
// pointers the value form.
func (g *generator) genUnmarshalCall(r *goRenderer, lhs string, typ types.Type) {
	fnName := g.ensureUnmarshaler(typ)
	basic, ok := typ.(*types.Basic)
	var isSliceByte bool
	if !ok {
		var slice *types.Slice
		_ = slice
		if slice, ok = typ.(*types.Slice); ok {
			basic, ok = typ.(*types.Basic)
			if ok {
				ok = basic.Kind() == types.Byte
				isSliceByte = true
			}
		}
	}
	if !ok {
		if isUnmarshalValueForm(typ) {
			if g.needsBuffer(typ) {
				r.L(`$0, src = $1(src, lim, buf)`, lhs, fnName)
				return
			}

			r.L(`$0, src = $1(src, lim)`, lhs, fnName)
			return
		}

		if g.needsBuffer(typ) {
			r.L(`src = $0(&$1, src, lim, buf)`, fnName, lhs)
			return
		}
		r.L(`src = $0(&$1, src, lim)`, fnName, lhs)
		return
	}

	r.Imports().MsgpUnsafe().Ref("msgpu")
	if isSliceByte {
		r.L(`$0, src = $msgpu.TakeBytes(src, lim, buf)`, lhs)
		return
	}
	basicHandler, ok := basicUnmarshalTable[basic.Kind()]
	if !ok {
		panic("unsupported basic type " + basic.String())
	}

	if g.needsBuffer(typ) {
		r.L(`$0, src = $msgpu.$1(src, lim, buf)`, lhs, basicHandler.takeFn)
		return
	}

	r.L(`$0, src = $msgpu.$1(src, lim)`, lhs, basicHandler.takeFn)
}

func (g *generator) emitUnmarshaler(typ types.Type) error {
	name := g.fnNames[typ.String()]
	g.funcs.N()

	if ptr, ok := typ.(*types.Pointer); ok {
		return g.emitUnmarshalerPointer(name, ptr)
	}

	switch u := typ.Underlying().(type) {
	case *types.Basic:
		return nil
	case *types.Struct:
		return g.emitUnmarshalerStruct(name, typ, u)
	case *types.Slice:
		return g.emitUnmarshalerSlice(name, typ, u)
	case *types.Map:
		return g.emitUnmarshalerMap(name, typ, u)
	default:
		g.errorf(token.NoPos, "unsupported unmarshal type: %s", typ.String())
		return errors.Newf("unsupported unmarshal type: %s", typ.String())
	}
}

func (g *generator) emitUnmarshalerStruct(name string, typ types.Type, st *types.Struct) error {
	r := g.funcs
	r.Imports().MsgpUnsafe().Ref("msgpu")
	r.Imports().Add("unsafe").Ref("unsafe")
	r.N()

	if g.needsBuffer(typ) {
		r.L(`func $0(dst *$1, src $unsafe.Pointer, lim int, buf *$msgpu.SafeBuffer) $unsafe.Pointer {`, name, typ)
	} else {
		r.L(`func $0(dst *$1, src $unsafe.Pointer, lim int) $unsafe.Pointer {`, name, typ)
	}
	r.L(`    var sz  int`)
	r.L(`    orig := src`)
	r.L(`    origLim := lim`)
	r.N()
	r.L(`    sz, src = $msgpu.TakeMapHeader(src, lim)`)
	r.L(`    lim = origLim - int(uintptr(src) - uintptr(orig))`)
	r.N()
	r.L(`    var key string`)
	r.L(`    for i := 0; i < sz; i++ {`)
	r.L(`        key, src = $msgpu.TakeStringZC(src, lim)`)
	r.L(`        lim = origLim - int(uintptr(src) - uintptr(orig))`)
	r.N()
	r.L(`        switch key {`)

	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		if !field.Exported() {
			continue
		}

		tagData := reflect.StructTag(st.Tag(i))
		msgName := tagData.Get(msgpTag)
		if msgName == "-" {
			continue
		}
		if msgName == "" {
			msgName = field.Name()
		}

		r.L(`        case $0:`, gogh.Q(msgName))
		g.genUnmarshalCall(r, "dst."+field.Name(), field.Type())
	}

	r.L(`        default:`)
	r.L(`            panic($msgpu.ErrorUnknownField)`)
	r.L(`        }`)
	r.L(`        lim = origLim - int(uintptr(src) - uintptr(orig))`)
	r.L(`    }`)
	r.N()
	r.L(`    return src`)
	r.L(`}`)
	r.N()

	return nil
}

func (g *generator) emitUnmarshalerSlice(name string, typ types.Type, sl *types.Slice) error {
	r := g.funcs
	r.Imports().MsgpUnsafe().Ref("msgpu")
	r.N()

	if g.needsBuffer(typ) {
		r.L(`func $0(dst *$1, src $unsafe.Pointer, lim int, buf *$msgpu.SafeBuffer) $unsafe.Pointer {`, name, typ)
	} else {
		r.L(`func $0(dst *$1, src $unsafe.Pointer, lim int) $unsafe.Pointer {`, name, typ)
	}
	r.L(`    var sz int`)
	r.L(`    orig := src`)
	r.L(`    origLim := lim`)
	r.N()
	r.L(`    sz, src = $msgpu.TakeSliceHeader(src, lim)`)
	r.L(`    lim = origLim - int(uintptr(src) - uintptr(orig))`)
	r.L(`    result := make($0, sz)`, typ)
	r.L(`    for i := 0; i < sz; i++ {`)
	g.genUnmarshalCall(r, "result[i]", sl.Elem())
	r.L(`        lim = origLim - int(uintptr(src) - uintptr(orig))`)
	r.L(`    }`)
	r.L(`    *dst = result`)
	r.N()
	r.L(`    return src`)
	r.L(`}`)
	r.N()

	return nil
}

func (g *generator) emitUnmarshalerMap(name string, typ types.Type, m *types.Map) error {
	keyType, ok := m.Key().Underlying().(*types.Basic)
	if !ok || keyType.Kind() != types.String {
		g.errorf(token.NoPos, "only map[string]T is supported for unmarshal, got key type %s", m.Key().String())
		return errors.Newf("unsupported map key type %s", m.Key().String())
	}

	r := g.funcs
	r.Imports().MsgpUnsafe().Ref("msgpu")
	r.Imports().Add("unsafe").Ref("unsafe")
	r.N()

	r.L(`func $0(dst *$1, src $unsafe.Pointer, lim int, buf *$msgpu.SafeBuffer) $unsafe.Pointer {`, name, typ)

	r.L(`    var sz int`)
	r.L(`    orig := src`)
	r.L(`    origLim := lim`)
	r.L(`    sz, src = $msgpu.TakeMapHeader(src, lim)`)
	r.L(`    lim = origLim - int(uintptr(src) - uintptr(orig))`)
	r.L(`    result := make($0, sz)`, typ)
	r.L(`    var key $0`, m.Key())
	r.L(`    for i := 0; i < sz; i++ {`)
	r.L(`        key, src = $msgpu.TakeString(src, lim, buf)`)
	r.L(`        lim = origLim - int(uintptr(src) - uintptr(orig))`)
	r.N()
	r.L(`        var value $0`, m.Elem())
	g.genUnmarshalCall(r, "value", m.Elem())
	r.L(`        result[key] = value`)
	r.L(`        lim = origLim - int(uintptr(src) - uintptr(orig))`)
	r.L(`    }`)
	r.L(`    *dst = result`)
	r.N()
	r.L(`    return src`)
	r.L(`}`)
	r.N()

	return nil
}

func (g *generator) emitUnmarshalerPointer(name string, ptr *types.Pointer) error {
	r := g.funcs
	r.Imports().MsgpUnsafe().Ref("msgpu")
	r.Imports().Add("unsafe").Ref("unsafe")
	r.N()

	elem := ptr.Elem()
	elemFn := g.ensureUnmarshaler(elem)

	if g.needsBuffer(elem) {
		r.L(`func $0(src $unsafe.Pointer, lim int, buf *$msgpu.SafeBuffer) (*$1, $unsafe.Pointer) {`, name, elem)
	} else {
		r.L(`func $0(src $unsafe.Pointer, lim int) (*$1, $unsafe.Pointer) {`, name, elem)
	}

	r.L(`    if isNil, src := $msgpu.IsNil(src, lim) {`)
	r.L(`        return nil, src`)
	r.L(`    }`)
	r.N()
	r.L(`    value := new($0)`, elem)
	if isUnmarshalValueForm(elem) {
		r.L(`    var raw $0`, elem)
		r.L(`    raw, src = $0(src, lim)`, elemFn)
		r.L(`    *value = raw`)
		r.L(`    return value, src`)
	} else {
		r.L(`    src = $0(value, src, lim)`, elemFn)
		r.L(`    return value, src`)
	}
	r.L(`}`)
	r.N()

	return nil
}
