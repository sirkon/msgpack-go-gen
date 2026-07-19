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

	// simpleAllocCache stores types who need msgpunsafe.SafeBuffer for their
	// unmarshal.
	simpleAllocCache map[types.Type]bool
}

func newGenerator(fset *token.FileSet, pkg *packages.Package) *generator {
	return &generator{
		fset:             fset,
		pkg:              pkg,
		recvNames:        map[*types.Named]string{},
		fnNames:          map[string]string{},
		usedNames:        map[string]struct{}{},
		simpleAllocCache: map[types.Type]bool{},
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

// needsBuffer рекурсивно проверяет, содержит ли тип строки или слайсы байт,
// требующие копирования в SafeBuffer. Результаты кэшируются.
func (g *generator) needsBuffer(t types.Type) bool {
	// 1. Проверяем кэш, чтобы не гонять рекурсию по второму кругу
	if res, ok := g.simpleAllocCache[t]; ok {
		return res
	}

	// Защита от бесконечной рекурсии (циклические зависимости структур через поинтеры).
	// Временно сохраняем false. Если в процессе обхода мы вернемся к этому же типу,
	// значит, зацикливание произошло не на строке/байтах. Если строка найдется в другой ветке,
	// кэш перезапишется финальным правильным результатом true.
	g.simpleAllocCache[t] = false

	switch typ := t.Underlying().(type) {
	// Базовые типы Go (int, string, bool...)
	case *types.Basic:
		// Если это строка (string) — буфер нужен 100%
		if typ.Kind() == types.String {
			g.simpleAllocCache[t] = true
			return true
		}

	// Слайсы ([]Sub, []byte, []int...)
	case *types.Slice:
		// Особый случай: []byte (в go/types это Slice от Basic типа Byte)
		if basic, ok := typ.Elem().Underlying().(*types.Basic); ok && basic.Kind() == types.Byte {
			g.simpleAllocCache[t] = true
			return true
		}
		// Для остальных слайсов (например, []Sub) проверяем тип элемента
		if g.needsBuffer(typ.Elem()) {
			g.simpleAllocCache[t] = true
			return true
		}

	// Динамические мапы (map[string]Sub...)
	case *types.Map:
		// Проверяем и ключ мапы, и её значение
		if g.needsBuffer(typ.Key()) || g.needsBuffer(typ.Elem()) {
			g.simpleAllocCache[t] = true
			return true
		}

	// Поинтеры (*Sub, *string...)
	case *types.Pointer:
		if g.needsBuffer(typ.Elem()) {
			g.simpleAllocCache[t] = true
			return true
		}

	// Самое важное: структуры, сгенерированные для полей
	case *types.Struct:
		for i := 0; i < typ.NumFields(); i++ {
			field := typ.Field(i)
			if g.needsBuffer(field.Type()) {
				g.simpleAllocCache[t] = true
				return true
			}
		}

	// Массивы фиксированной длины ([16]byte, [4]Sub...)
	case *types.Array:
		if g.needsBuffer(typ.Elem()) {
			g.simpleAllocCache[t] = true
			return true
		}
	}

	// Если дошли сюда — тип «чистый» (только инты, флоаты, булы и т.д.), буфер не нужен
	return g.simpleAllocCache[t]
}
