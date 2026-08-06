package sample

import (
	"math"
	"testing"

	"github.com/sirkon/deepequal"
	"github.com/sirkon/errors"
	"github.com/sirkon/msgpunsafe"
	"github.com/vmihailenco/msgpack/v5"
)

func TestDataMarshaler(t *testing.T) {
	dat := Data{
		Name:  "name",
		Count: 12,
		Subs: []Sub{
			{
				Key:    "key-1",
				Active: true,
			},
			{
				Key:    "key-2",
				Active: false,
			},
			{
				Key:    "key-3",
				Active: false,
			},
			{
				Key:    "key-4",
				Active: true,
			},
		},
		Internal: struct {
			Value float32 `msgpack:"value"`
		}{
			Value: math.Pi,
		},
		Weights: []uint64{1, 2, 3, 999},
		Meta: map[string]Sub{
			"1": {
				Key:    "k",
				Active: true,
			},
			"2": {
				Key:    "kk",
				Active: false,
			},
		},
		Flags: map[string]bool{
			"1": false,
			"2": true,
		},
	}

	data, err := dat.MarshalMsgpack(nil)
	if err != nil {
		t.Error(errors.Wrap(err, "marshal data"))
	}

	var got Data
	if err := msgpack.Unmarshal(data, &got); err != nil {
		t.Error(errors.Wrap(err, "unmarshal packed data"))
	}

	deepequal.SideBySide(t, "structure Data", dat, got)
}

func TestRequestMarshaler(t *testing.T) {
	req := Request{
		Hash:  "hash-1",
		Value: "value-1",
	}

	const (
		functionName = "function-name"
		reqID        = "request-id"
	)
	data, err := MarshalRequest(nil, &req, functionName, reqID)
	if err != nil {
		t.Fatal(errors.Wrap(err, "marshal request with additional fields"))
	}

	want := RequestCheck{
		FuncName: functionName,
		ReqID:    reqID,
		Hash:     req.Hash,
		Value:    req.Value,
	}
	var got RequestCheck
	if err := msgpack.Unmarshal(data, &got); err != nil {
		t.Fatal(errors.Wrap(err, "unmarshal packed data"))
	}

	deepequal.SideBySide(t, "structure Request", want, got)
}

func TestDataUnmarshaler(t *testing.T) {
	want := Data{
		Name:  "name",
		Count: 12,
		Subs: []Sub{
			{Key: "key-1", Active: true},
			{Key: "key-2", Active: false},
			{Key: "key-3", Active: false},
			{Key: "key-4", Active: true},
		},
		Internal: struct {
			Value float32 `msgpack:"value"`
		}{
			Value: math.Pi,
		},
		Weights: []uint64{1, 2, 3, 999},
		Meta: map[string]Sub{
			"1": {Key: "k", Active: true},
			"2": {Key: "kk", Active: false},
		},
		Flags: map[string]bool{
			"1": false,
			"2": true,
		},
	}

	// Marshal with an independent implementation, decode with the generated one.
	data, err := msgpack.Marshal(&want)
	if err != nil {
		t.Fatal(errors.Wrap(err, "marshal data with reference implementation"))
	}

	var got Data
	if err := got.UnmarshalMsgpack(data, msgpunsafe.NewSafeBuffer(128)); err != nil {
		t.Fatal(errors.Wrap(err, "unmarshal data with generated implementation"))
	}

	deepequal.SideBySide(t, "structure Data", want, got)
}

func TestDataMarshalUnmarshalRoundTrip(t *testing.T) {
	want := Data{
		Name:    "round-trip",
		Count:   42,
		Subs:    []Sub{{Key: "a", Active: true}},
		Weights: []uint64{7, 8, 9},
		Meta:    map[string]Sub{"x": {Key: "y", Active: true}},
		Flags:   map[string]bool{"on": true},
	}
	want.Internal.Value = 1.5

	data, err := want.MarshalMsgpack(nil)
	if err != nil {
		t.Fatal(errors.Wrap(err, "marshal data with generated implementation"))
	}

	var got Data
	if err := got.UnmarshalMsgpack(data, msgpunsafe.NewSafeBuffer(128)); err != nil {
		t.Fatal(errors.Wrap(err, "unmarshal data with generated implementation"))
	}

	deepequal.SideBySide(t, "structure Data", want, got)
}

// TestScalars exercises the needsBuffer==false path: Scalars has no string or
// []byte fields anywhere, so its unmarshaler free functions take no SafeBuffer
// and the top-level method calls them without one.
func TestScalarsUnmarshaler(t *testing.T) {
	want := Scalars{
		Count:    7,
		Big:      -1 << 40,
		Unsigned: 1 << 40,
		Pi:       math.Pi,
		On:       true,
		Negs:     []int{-5, 0, 5, 1000},
	}
	want.Inner.X = 11
	want.Inner.Y = -22

	// Marshal with an independent implementation, decode with the generated one.
	data, err := msgpack.Marshal(&want)
	if err != nil {
		t.Fatal(errors.Wrap(err, "marshal scalars with reference implementation"))
	}

	var got Scalars
	if err := got.UnmarshalMsgpack(data, msgpunsafe.NewSafeBuffer(128)); err != nil {
		t.Fatal(errors.Wrap(err, "unmarshal scalars with generated implementation"))
	}

	deepequal.SideBySide(t, "structure Scalars", want, got)
}

func TestScalarsMarshalUnmarshalRoundTrip(t *testing.T) {
	want := Scalars{
		Count:    42,
		Big:      1 << 50,
		Unsigned: 1 << 50,
		Pi:       math.E,
		On:       false,
		Negs:     []int{1, 2, 3, 4, 5},
	}
	want.Inner.X = 100
	want.Inner.Y = 200

	data, err := want.MarshalMsgpack(nil)
	if err != nil {
		t.Fatal(errors.Wrap(err, "marshal scalars with generated implementation"))
	}

	var got Scalars
	if err := got.UnmarshalMsgpack(data, msgpunsafe.NewSafeBuffer(128)); err != nil {
		t.Fatal(errors.Wrap(err, "unmarshal scalars with generated implementation"))
	}

	deepequal.SideBySide(t, "structure Scalars", want, got)
}
