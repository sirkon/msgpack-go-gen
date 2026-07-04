package main

import (
	"go/token"
	"go/types"
	"strings"

	"github.com/sirkon/gogh"
)

const (
	fieldCountCorrectorMethod = "correctFieldCount"
)

type goRenderer = gogh.GoRenderer[*gogh.Imports]

type generator struct {
	fset *token.FileSet
}

func (g *generator) generateMarshaler(r *goRenderer, name string, desc *types.Named) error {
	var recv string
	var isThereACorrector bool
	for method := range desc.Methods() {
		if recv == "" {
			recv = method.Origin().Signature().Recv().Origin().Name()
		}

		if method.Name() == fieldCountCorrectorMethod {
			isThereACorrector = true
		}
	}

	if recv == "" {
		recv = strings.ToLower(string([]rune(name)[0]))
	}

	r = r.Scope()
	r.Let("recv", r.Uniq(recv))

	// Cound number of fields.
	s := desc.Underlying().(*types.Struct)
	for i := 0; i < s.NumFields(); i++ {
		field := s.Field(i)
		if !field.Exported() {
			continue
		}

		tag := s.Tag(i)
	}

	r.L(`func ($recv *$0) MarshalMsgpack(@dst []byte) ([]byte, error) {`)
	r.L(`}`)
}

func (g *generator) generateStruct(r *goRenderer, desc *types.Named) error {

}
