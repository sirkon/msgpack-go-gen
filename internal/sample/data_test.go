package sample

import (
	"math"
	"testing"

	"github.com/sirkon/deepequal"
	"github.com/sirkon/errors"
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
