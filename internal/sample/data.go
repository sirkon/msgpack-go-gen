package sample

import (
	"github.com/tinylib/msgp/msgp"
)

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

// Scalars is a struct with only fixed-size fields — no string or []byte
// anywhere, recursively. It therefore takes the needsBuffer==false path through
// the generator, exercising the no-buffer unmarshaler signatures.
type Scalars struct {
	Count    int     `msgpack:"count"`
	Big      int64   `msgpack:"big"`
	Unsigned uint64  `msgpack:"unsigned"`
	Pi       float64 `msgpack:"pi"`
	On       bool    `msgpack:"on"`
	Negs     []int   `msgpack:"negs"`
	Inner    struct {
		X int `msgpack:"x"`
		Y int `msgpack:"y"`
	} `msgpack:"inner"`
}

type Request struct {
	Hash  string `msgpack:"hash"`
	Value string `msgpack:"payload"`
}

type RequestCheck struct {
	FuncName string `msgpack:"funcname"`
	ReqID    string `msgpack:"reqid"`
	Hash     string `msgpack:"hash"`
	Value    string `msgpack:"payload"` // Мы не поддерживаем []byte-ы по-настоящему. Пока.
}

func (r *Request) alterFieldCount() int {
	return 2
}

func MarshalRequest(dst []byte, req *Request, funcname, reqid string) ([]byte, error) {
	dst, err := req.MarshalMsgpack(dst)
	if err != nil {
		return nil, err
	}

	dst = msgp.AppendString(dst, "funcname")
	dst = msgp.AppendString(dst, funcname)

	dst = msgp.AppendString(dst, "reqid")
	dst = msgp.AppendString(dst, reqid)

	return dst, nil
}
