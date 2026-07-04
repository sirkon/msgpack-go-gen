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
