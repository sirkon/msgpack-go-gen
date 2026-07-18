package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/sirkon/errors"
	"github.com/sirkon/gogh"
	"github.com/sirkon/message"
	"golang.org/x/tools/go/packages"
)

const (
	fieldCountAlterationMethod = "alterFieldCount"
	msgpTag                    = "msgpack"
)

type goRenderer = gogh.GoRenderer[*importer]

type generator struct {
	fset *token.FileSet
	pkg  *packages.Package

	// recvNames caches computed receiver names per named type so that both the
	// marshaler and the unmarshaler use the same receiver identifier.
	recvNames map[*types.Named]string

	// fnNames maps a type key (types.Type.String()) onto the name of the free
	// unmarshal function generated for it. It is package-wide so an unmarshaler
	// generated once is reused everywhere.
	fnNames map[string]string
	// usedNames tracks every free function name already taken to keep them
	// unique across the whole package.
	usedNames map[string]struct{}
	// queue holds types whose free unmarshal function is registered but whose
	// body has not been emitted yet. Emission is drained sequentially to avoid
	// interleaving function bodies.
	queue []types.Type

	// funcs is the renderer for the currently processed file where private free
	// functions are written. pub is a "laZy" renderer (see gogh Z) referencing a
	// block placed before funcs, so that all public methods end up before the
	// private free functions.
	funcs *goRenderer
	pub   *goRenderer
}

func newGenerator(fset *token.FileSet, pkg *packages.Package) *generator {
	return &generator{
		fset:      fset,
		pkg:       pkg,
		recvNames: map[*types.Named]string{},
		fnNames:   map[string]string{},
		usedNames: map[string]struct{}{},
	}
}

// receiver returns the receiver identifier to use for methods of the given
// named type. The value is cached so that MarshalMsgpack and UnmarshalMsgpack
// share the same receiver name.
func (g *generator) receiver(name string, desc *types.Named) string {
	if recv, ok := g.recvNames[desc]; ok {
		return recv
	}

	var recv string
	for method := range desc.Methods() {
		recv = method.Origin().Signature().Recv().Origin().Name()
		break
	}
	if recv == "" {
		recv = strings.ToLower(string([]rune(name)[0]))
	}

	g.recvNames[desc] = recv
	return recv
}

func (g *generator) errorf(pos token.Pos, format string, args ...interface{}) {
	message.Error(g.fset.Position(pos), fmt.Sprintf(format, args...))
}

func isValidFieldCountCorrectorResult(sig *types.Signature) bool {
	if sig.Results().Len() != 1 {
		return false
	}

	typ, ok := sig.Results().At(0).Type().(*types.Basic)
	if !ok {
		return false
	}

	return typ.Kind() == types.Int
}

func (g *generator) getFieldCoundOffset(method *types.Func) (int, error) {
	pkg := g.pkg
	methodPos := method.Pos()
	// Ищем файл AST, в котором физически находится метод
	for _, file := range pkg.Syntax {
		// Проверяем, попадает ли позиция метода в диапазон этого файла
		if methodPos >= file.Pos() && methodPos <= file.End() {

			// Обходим декларации в файле
			for _, decl := range file.Decls {
				funcDecl, ok := decl.(*ast.FuncDecl)
				if !ok || funcDecl.Name.Pos() != methodPos {
					continue
				}

				// Мы нашли тело метода! Ищем оператор return
				if funcDecl.Body == nil || len(funcDecl.Body.List) == 0 {
					return 0, errors.New("empty method body")
				}

				if len(funcDecl.Body.List) != 1 {
					return 0, errors.New("method body must have return only")
				}

				returnStmt, ok := funcDecl.Body.List[0].(*ast.ReturnStmt)
				if !ok || len(returnStmt.Results) != 1 {
					return 0, errors.New("method must return single ")
				}

				// Проверяем, что возвращается обычное число (целочисленный литерал)
				lit, ok := returnStmt.Results[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					return 0, errors.New(fieldCountAlterationMethod + " must return a static integer literal")
				}

				// Превращаем строку "5" в честный int
				returnLit, err := strconv.ParseInt(lit.Value, 10, 64)
				if err != nil {
					return 0, errors.Wrap(err, "parse return value")
				}

				return int(returnLit), nil
			}
		}
	}

	return 0, errors.New("method AST declaration not found")
}
