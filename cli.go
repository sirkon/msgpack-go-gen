package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"strings"

	"github.com/sirkon/errors"
)

const appName = "msgpack-go-gen"

type CLI struct {
	Package string         `short:"p" help:"Package directory path." default:"." required:""`
	Structs []StructPolicy `arg:"" required:"" help:"Structure 'policy' to generate msgpack encoders and/or decoders for."`
}

type StructPolicy struct {
	Name      string
	Marshal   bool
	Unmarshal bool
}

func (s StructPolicy) String() string {
	return s.Name
}

func (s *StructPolicy) UnmarshalText(raw []byte) error {
	if len(raw) == 0 {
		return errors.New("struct policy must not be empty")
	}

	text := string(raw)
	split := strings.Split(text, ":")
	var name string
	var policyLit []rune

	switch len(split) {
	case 1:
		name = split[0]
		// Если маркер опущен, по дефолту генерируем всё
		s.Marshal = true
		s.Unmarshal = true
	case 2:
		name = split[0]
		policyLit = []rune(split[1])

		if len(policyLit) != 2 {
			return errors.Newf("invalid rules length in the %q policy, expected 2 chars (e.g. +-, ++)", text)
		}

		switch policyLit[0] {
		case '+':
			s.Marshal = true
		case '-':
			s.Marshal = false
		default:
			return errors.Newf("invalid rule for marshaling in the %q policy", text)
		}

		switch policyLit[1] {
		case '+':
			s.Unmarshal = true
		case '-':
			s.Unmarshal = false
		default:
			return errors.Newf("invalid rule for unmarshaling in the %q policy", text)
		}
	default:
		return errors.Newf("invalid struct policy %q", text)
	}

	expr, err := parser.ParseExpr(name)
	if err != nil {
		return fmt.Errorf("parse name of policy to check whether it is a valid Go identifier: %w", err)
	}

	if _, ok := expr.(*ast.Ident); !ok {
		return fmt.Errorf("invalid struct name %q of the %q policy, must be valid Go identifier", name, text)
	}

	s.Name = name
	return nil
}
