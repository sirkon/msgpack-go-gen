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
	readFn string // name of the msgp.Read*Bytes function
}

var basicUnmarshalTable = map[types.BasicKind]basicUnmarshalInfo{
	types.String:  {base: "String", goType: "string", readFn: "ReadStringBytes"},
	types.Bool:    {base: "Bool", goType: "bool", readFn: "ReadBoolBytes"},
	types.Int:     {base: "Int", goType: "int", readFn: "ReadIntBytes"},
	types.Int8:    {base: "Int8", goType: "int8", readFn: "ReadInt8Bytes"},
	types.Int16:   {base: "Int16", goType: "int16", readFn: "ReadInt16Bytes"},
	types.Int32:   {base: "Int32", goType: "int32", readFn: "ReadInt32Bytes"},
	types.Int64:   {base: "Int64", goType: "int64", readFn: "ReadInt64Bytes"},
	types.Uint:    {base: "Uint", goType: "uint", readFn: "ReadUintBytes"},
	types.Uint8:   {base: "Uint8", goType: "uint8", readFn: "ReadUint8Bytes"},
	types.Uint16:  {base: "Uint16", goType: "uint16", readFn: "ReadUint16Bytes"},
	types.Uint32:  {base: "Uint32", goType: "uint32", readFn: "ReadUint32Bytes"},
	types.Uint64:  {base: "Uint64", goType: "uint64", readFn: "ReadUint64Bytes"},
	types.Float32: {base: "Float32", goType: "float32", readFn: "ReadFloat32Bytes"},
	types.Float64: {base: "Float64", goType: "float64", readFn: "ReadFloat64Bytes"},
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
	m.N()
	m.Let("recv", m.Uniq(g.receiver(name, desc)))
	m.L(`func ($recv *$0) UnmarshalMsgpack(src []byte) error {`, desc)
	m.L(`    _, err := $0($recv, src)`, fnName)
	m.L(`    return err`)
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

	fnName := g.uniqueUnmarshalerName(g.unmarshalerBaseName(typ))
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
	if isUnmarshalValueForm(typ) {
		r.L(`srcOffset, $0, err = $1(src)`, lhs, fnName)
		return
	}
	r.L(`srcOffset, err = $0(&$1, src)`, fnName, lhs)
}

func (g *generator) emitUnmarshaler(typ types.Type) error {
	name := g.fnNames[typ.String()]
	g.funcs.N()

	if ptr, ok := typ.(*types.Pointer); ok {
		return g.emitUnmarshalerPointer(name, ptr)
	}

	switch u := typ.Underlying().(type) {
	case *types.Basic:
		return g.emitUnmarshalerScalar(name, typ, u)
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

func (g *generator) emitUnmarshalerScalar(name string, typ types.Type, b *types.Basic) error {
	info, ok := basicUnmarshalTable[b.Kind()]
	if !ok {
		g.errorf(token.NoPos, "unsupported basic kind for unmarshal: %s", typ.String())
		return errors.Newf("unsupported basic kind: %s", typ.String())
	}

	r := g.funcs
	r.Imports().Msgp().Ref("msgp")

	r.N()

	r.L(`func $0(src []byte) (offset int, value $1, err error) {`, name, typ)
	r.L(`    var raw $0`, info.goType)
	r.L(`    var tail []byte`)
	r.L(`    raw, tail, err = $msgp.$0(src)`, info.readFn)
	r.L(`    if err != nil {`)
	r.L(`        return 0, value, err`)
	r.L(`    }`)
	r.L(`    return len(src) - len(tail), $0(raw), nil`, typ)
	r.L(`}`)
	r.N()

	return nil
}

func (g *generator) emitUnmarshalerStruct(name string, typ types.Type, st *types.Struct) error {
	r := g.funcs
	r.Imports().Msgp().Ref("msgp")
	r.N()

	keyFn := g.ensureUnmarshaler(types.Typ[types.String])

	r.L(`func $0(dst *$1, src []byte) (int, error) {`, name, typ)
	r.L(`    orig := src`)
	r.L(`    sz, tail, err := $msgp.ReadMapHeaderBytes(src)`)
	r.L(`    if err != nil {`)
	r.L(`        return 0, err`)
	r.L(`    }`)
	r.L(`    src = tail`)
	r.N()
	r.L(`    var srcOffset int`)
	r.L(`    var key string`)
	r.L(`    for i := uint32(0); i < sz; i++ {`)
	r.L(`        srcOffset, key, err = $0(src)`, keyFn)
	r.L(`        if err != nil {`)
	r.L(`            return 0, err`)
	r.L(`        }`)
	r.L(`        src = src[srcOffset:]`)
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

		r.L(`        case $0:`, strconv.Quote(msgName))
		g.genUnmarshalCall(r, "dst."+field.Name(), field.Type())
	}

	r.L(`        default:`)
	r.Imports().Add("fmt").Ref("fmt")
	r.L(`            return len(orig) - len(src), $fmt.Errorf("unknown field %q", key)`)
	r.L(`        }`)
	r.L(`        if err != nil {`)
	r.L(`            return 0, err`)
	r.L(`        }`)
	r.L(`        src = src[srcOffset:]`)
	r.L(`    }`)
	r.N()
	r.L(`    return len(orig) - len(src), nil`)
	r.L(`}`)
	r.N()

	return nil
}

func (g *generator) emitUnmarshalerSlice(name string, typ types.Type, sl *types.Slice) error {
	r := g.funcs
	r.Imports().Msgp().Ref("msgp")
	r.N()

	r.L(`func $0(dst *$1, src []byte) (int, error) {`, name, typ)
	r.L(`    orig := src`)
	r.L(`    sz, tail, err := $msgp.ReadArrayHeaderBytes(src)`)
	r.L(`    if err != nil {`)
	r.L(`        return 0, err`)
	r.L(`    }`)
	r.L(`    src = tail`)
	r.L(`    result := make($0, sz)`, typ)
	r.L(`    var srcOffset int`)
	r.L(`    for i := uint32(0); i < sz; i++ {`)
	g.genUnmarshalCall(r, "result[i]", sl.Elem())
	r.L(`        if err != nil {`)
	r.L(`            return 0, err`)
	r.L(`        }`)
	r.L(`        src = src[srcOffset:]`)
	r.L(`    }`)
	r.L(`    *dst = result`)
	r.N()
	r.L(`    return len(orig) - len(src), nil`)
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
	r.Imports().Msgp().Ref("msgp")
	r.N()

	keyFn := g.ensureUnmarshaler(m.Key())

	r.L(`func $0(dst *$1, src []byte) (int, error) {`, name, typ)
	r.L(`    orig := src`)
	r.L(`    sz, tail, err := $msgp.ReadMapHeaderBytes(src)`)
	r.L(`    if err != nil {`)
	r.L(`        return 0, err`)
	r.L(`    }`)
	r.L(`    src = tail`)
	r.L(`    result := make($0, sz)`, typ)
	r.L(`    var srcOffset int`)
	r.L(`    var key $0`, m.Key())
	r.L(`    for i := uint32(0); i < sz; i++ {`)
	r.L(`        srcOffset, key, err = $0(src)`, keyFn)
	r.L(`        if err != nil {`)
	r.L(`            return 0, err`)
	r.L(`        }`)
	r.L(`        src = src[srcOffset:]`)
	r.N()
	r.L(`        var value $0`, m.Elem())
	g.genUnmarshalCall(r, "value", m.Elem())
	r.L(`        if err != nil {`)
	r.L(`            return 0, err`)
	r.L(`        }`)
	r.L(`        src = src[srcOffset:]`)
	r.L(`        result[key] = value`)
	r.L(`    }`)
	r.L(`    *dst = result`)
	r.N()
	r.L(`    return len(orig) - len(src), nil`)
	r.L(`}`)
	r.N()

	return nil
}

func (g *generator) emitUnmarshalerPointer(name string, ptr *types.Pointer) error {
	r := g.funcs
	r.Imports().Msgp().Ref("msgp")
	r.N()

	elem := ptr.Elem()
	elemFn := g.ensureUnmarshaler(elem)

	r.L(`func $0(src []byte) (int, $1, error) {`, name, ptr)
	r.L(`    if $msgp.IsNil(src) {`)
	r.L(`        tail, err := $msgp.ReadNilBytes(src)`)
	r.L(`        if err != nil {`)
	r.L(`            return 0, nil, err`)
	r.L(`        }`)
	r.L(`        return len(src) - len(tail), nil, nil`)
	r.L(`    }`)
	r.N()
	r.L(`    value := new($0)`, elem)
	if isUnmarshalValueForm(elem) {
		r.L(`    srcOffset, raw, err := $0(src)`, elemFn)
		r.L(`    if err != nil {`)
		r.L(`        return 0, nil, err`)
		r.L(`    }`)
		r.L(`    *value = raw`)
		r.L(`    return srcOffset, value, nil`)
	} else {
		r.L(`    srcOffset, err := $0(value, src)`, elemFn)
		r.L(`    if err != nil {`)
		r.L(`        return 0, nil, err`)
		r.L(`    }`)
		r.L(`    return srcOffset, value, nil`)
	}
	r.L(`}`)
	r.N()

	return nil
}
