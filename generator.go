package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
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
}

func (g *generator) errorf(pos token.Pos, format string, args ...interface{}) {
	message.Error(g.fset.Position(pos), fmt.Sprintf(format, args...))
}

func (g *generator) generateMarshaler(r *goRenderer, name string, desc *types.Named) error {
	var recv string
	var fieldCountCorrector fieldCountCorrectMethod

serviceLoop:
	for method := range desc.Methods() {
		if recv == "" {
			recv = method.Origin().Signature().Recv().Origin().Name()
		}

		if method.Name() == fieldCountAlterationMethod {
			sig := method.Origin().Signature()

			if !isValidFieldCountCorrectorResult(sig) {
				g.errorf(method.Pos(), "method %s must return int value", method.Name())
				return errors.New("invalid field count correction method")
			}

			switch sig.Params().Len() {
			case 0:
				offset, err := g.getFieldCoundOffset(method)
				if err != nil {
					g.errorf(method.Pos(), "method %s must consist of the single statement `return XXX`", method.Name())
					return errors.Wrapf(err, "compute offset value defined in the %s method", fieldCountAlterationMethod)
				}
				fieldCountCorrector = &fieldCountCorrectMethodOffset{offset: offset}
				break serviceLoop

			case 1:
				basic, ok := sig.Params().At(0).Type().(*types.Basic)
				if ok {
					if basic.Kind() != types.Int {
						ok = false
					}
				}
				if ok {
					fieldCountCorrector = &fieldCountCorrectMethodChange{}
					break serviceLoop
				}
			default:
			}

			g.errorf(method.Pos(), "method %s must receive either no value or int value", method.Name())
			return errors.New("invalid field count correction method")
		}
	}

	if recv == "" {
		recv = strings.ToLower(string([]rune(name)[0]))
	}

	r = r.Scope()
	r.Let("recv", r.Uniq(recv))

	r.L(`func ($recv *$0) MarshalMsgpack(@dst []byte) ([]byte, error) {`, name)
	if err := g.generateStruct(r, desc, fieldCountCorrector); err != nil {
		return errors.Wrap(err, "generate marshaler of "+desc.String())
	}
	r.L(`}`)

	return nil
}

func (g *generator) generateStruct(r *goRenderer, desc *types.Named, fcountCorrection fieldCountCorrectMethod) error {
	s := desc.Underlying().(*types.Struct)
	var fieldCount int
	fieldTags := map[string]string{}

	// Первый проход: сбор валидных полей структуры
	for i := 0; i < s.NumFields(); i++ {
		field := s.Field(i)
		if !field.Exported() {
			continue
		}

		tag := s.Tag(i)
		tagData := reflect.StructTag(tag)
		msgName := tagData.Get(msgpTag)

		if msgName == "-" {
			continue
		}
		if msgName == "" {
			msgName = field.Name()
		}

		fieldCount++
		fieldTags[field.Name()] = msgName
	}

	// Запись заголовка мапы (Твой блок расчета хедера)
	var fcount int
	switch v := fcountCorrection.(type) {
	case *fieldCountCorrectMethodOffset:
		fcount = fieldCount + v.offset
	case *fieldCountCorrectMethodChange:
		fcount = -1
	default:
		fcount = fieldCount
	}

	if fcount >= 0 {
		if fcount <= 15 {
			r.L(`    $dst = append($dst, $0)`, byte(0x80|(fcount&0x0F)))
		} else if fcount <= 65535 {
			r.L(`    $dst = append($dst, 0xDE, $0, $1)`, (fcount>>8)&0xFF, fcount&0xFF)
		} else {
			r.L(`    $dst = append($dst, 0xDF, $0, $1, $2, $3)`,
				(fcount>>24)&0xFF, (fcount>>16)&0xFF, (fcount>>8)&0xFF, fcount&0xFF,
			)
		}
	} else {
		r.L(`    fieldsCount := $recv.$0($1)`, fieldCountAlterationMethod, fieldCount)
		r.L(`    if fieldsCount <= 15 {`)
		r.L(`        $dst = append($dst, byte(0x80|(fieldsCount&0x0f)))`)
		r.L(`    } else if fieldsCount <= 65535 {`) // Исправили опечатку fieldCount -> fieldsCount
		r.L(`        $dst = append($dst, 0xde, byte(fieldsCount>>8), byte(fieldsCount))`)
		r.L(`    } else {`)
		r.L(`        $dst = append($dst, 0xdf, byte(fieldsCount>>24), byte(fieldsCount>>16), byte(fieldsCount>>8), byte(fieldsCount))`)
		r.L(`    }`)
	}

	// Второй проход: Генерируем запись ключей и инлайним значения
	for field := range s.Fields() {
		msgName, ok := fieldTags[field.Name()]
		if !ok {
			continue
		}

		r.Imports().Msgp().Ref("msgp")

		r.N() // Наш красивый r.L("") = r.N()
		r.L(`    // Поле: $0`, msgName)
		r.L(`    $dst = $msgp.AppendString($dst, $0)`, strconv.Quote(msgName))

		// Вызываем сквозной инлайнер genInlineValue, передавая ему базовый аксессор поля
		fieldAccessor := r.S("$recv.$0", field.Name())
		if err := g.genInlineValue(r, fieldAccessor, field.Type()); err != nil {
			return errors.Wrap(err, "inline field "+field.Name())
		}
	}

	r.N()
	r.L(`    return $dst, nil`)
	return nil
}

func (g *generator) genInlineValue(r *goRenderer, accessor string, typ types.Type) error {
	// Проверяем, является ли тип указателем, чтобы сгенерировать проверку на nil
	isPointer := false
	if ptr, ok := typ.(*types.Pointer); ok {
		isPointer = true
		typ = ptr.Elem()
	}

	if isPointer {
		r.L(`    if $0 == nil {`, accessor)
		r.L(`        $dst = $msgp.AppendNil($dst)`)
		r.L(`    } else {`)
		r = r.Scope()
		// Безопасный инлайновый разименованный аксессор для gogh
		accessor = r.S("(*$0)", accessor)
	}

	switch t := typ.Underlying().(type) {
	case *types.Basic:
		// Пакуем примитив на месте
		if err := g.genBasicValueInline(r, accessor, t.Kind()); err != nil {
			return err
		}

	case *types.Struct:
		// ТОТАЛЬНЫЙ ИНЛАЙН СТРУКТУРЫ
		var fieldsCount int
		type fMeta struct {
			goName  string
			msgName string
			t       types.Type
		}
		var validFields []fMeta

		// Собираем экспортируемые и валидные поля вложенной структуры
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if !f.Exported() {
				continue
			}
			tagData := reflect.StructTag(t.Tag(i))
			msgName := tagData.Get(msgpTag)
			if msgName == "-" {
				continue
			}
			if msgName == "" {
				msgName = f.Name()
			}

			fieldsCount++
			validFields = append(validFields, fMeta{goName: f.Name(), msgName: msgName, t: f.Type()})
		}

		// Записываем заголовок мапы для вложенного объекта
		if fieldsCount <= 15 {
			r.L(`        $dst = append($dst, byte(0x80|($0&0x0f)))`, fieldsCount)
		} else if fieldsCount <= 65535 {
			r.L(`        $dst = append($dst, 0xDE, byte($0>>8), byte($0))`, fieldsCount)
		} else {
			r.L(`        $dst = append($dst, 0xDF, byte($0>>24), byte($0>>16), byte($0>>8), byte($0))`, fieldsCount)
		}

		// Рекурсивно фигачим поля вложенной структуры слева направо
		for _, f := range validFields {
			r.N()
			r.L(`        $dst = $msgp.AppendString($dst, $0)`, strconv.Quote(f.msgName))

			// Собираем дочерний аксессор через r.S()
			nextAccessor := r.S("$0.$1", accessor, f.goName)
			if err := g.genInlineValue(r.Scope(), nextAccessor, f.t); err != nil {
				return err
			}
		}

	case *types.Slice:
		// ИНЛАЙН СЛАЙСА
		r.L(`        $dst = $msgp.AppendArrayHeader($dst, uint32(len($0)))`, accessor)

		valName := r.Uniq("v")
		r.L(`        for _, $0 := range $1 {`, valName, accessor)

		// Рекурсивно уходим в кодирование элементов слайса
		if err := g.genSliceElement(r.Scope(), valName, t.Elem()); err != nil {
			return err
		}
		r.L(`        }`)

	case *types.Map:
		// ИНЛАЙН МАПЫ map[K]V
		// Проверяем, что ключ мапы — это строка. Msgpack-мапы с другими ключами в RPC практически не используются.
		keyType, ok := t.Key().Underlying().(*types.Basic)
		if !ok || keyType.Kind() != types.String {
			g.errorf(token.NoPos, "only map[string]T is supported, got key type %s", t.Key().String())
			return errors.Newf("unsupported map key type %s", t.Key().String())
		}

		// Записываем заголовок мапы на основе её рантайм-длины len(m)
		r.L(`        $dst = $msgp.AppendMapHeader($dst, uint32(len($0)))`, accessor)

		// Генерируем уникальные имена переменных для ключа и значения итератора range
		kName := r.Uniq("k")
		vName := r.Uniq("v")

		r.L(`        for $0, $1 := range $2 {`, kName, vName, accessor)

		// Уходим в скоуп цикла мапы
		r = r.Scope()

		// А. Сначала всегда пишем ключ (в нашем случае это гарантированно string)
		r.L(`            $dst = $msgp.AppendString($dst, $0)`, kName)

		// Б. Затем рекурсивно вызываем кодирование значения мапы t.Elem()
		// Передаем vName в качестве нового аксессора
		if err := g.genMapElement(r, vName, t.Elem()); err != nil {
			return errors.Wrap(err, "generate element for map")
		}

		// Выходим из скоупа цикла
		r = r.Parent()
		r.L(`        }`)

	default:
		g.errorf(token.NoPos, "unsupported inline type: %s", typ.String())
		return errors.Newf("unsupported inline type: %s", typ.String())
	}

	if isPointer {
		r = r.Parent() // Твой жесткий паникующий .Parent()
		r.L(`    }`)
	}
	return nil
}

// genBasicValueInline генерирует вызовы msgp.Append... под конкретный примитив
func (g *generator) genBasicValueInline(r *goRenderer, val string, kind types.BasicKind) error {
	switch kind {
	case types.String:
		r.L(`        $dst = $msgp.AppendString($dst, $0)`, val)
	case types.Int, types.Int64:
		r.L(`        $dst = $msgp.AppendInt64($dst, int64($0))`, val)
	case types.Int32:
		r.L(`        $dst = $msgp.AppendInt32($dst, $0)`, val)
	case types.Int16:
		r.L(`        $dst = $msgp.AppendInt16($dst, $0)`, val)
	case types.Int8:
		r.L(`        $dst = $msgp.AppendInt8($dst, $0)`, val)
	case types.Uint, types.Uint64:
		r.L(`        $dst = $msgp.AppendUint64($dst, uint64($0))`, val)
	case types.Uint32:
		r.L(`        $dst = $msgp.AppendUint32($dst, $0)`, val)
	case types.Uint16:
		r.L(`        $dst = $msgp.AppendUint16($dst, $0)`, val)
	case types.Uint8:
		r.L(`        $dst = $msgp.AppendUint8($dst, $0)`, val)
	case types.Bool:
		r.L(`        $dst = $msgp.AppendBool($dst, $0)`, val)
	case types.Float64:
		r.L(`        $dst = $msgp.AppendFloat64($dst, $0)`, val)
	case types.Float32:
		r.L(`        $dst = $msgp.AppendFloat32($dst, $0)`, val)
	default:
		return errors.Newf("unsupported basic kind %d", kind)
	}
	return nil
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

func (g *generator) genSliceElement(r *goRenderer, valName string, elemType types.Type) error {
	// 1. Обрабатываем случай, если внутри слайса лежат указатели (например, []*string)
	isPointer := false
	if ptr, ok := elemType.(*types.Pointer); ok {
		isPointer = true
		elemType = ptr.Elem()
	}

	if isPointer {
		r.L(`        if $0 == nil {`, valName)
		r.L(`            $dst = $msgp.AppendNil($dst)`)
		r.L(`        } else {`)
		r = r.Scope()
		// Безопасный инлайновый разименованный аксессор для gogh
		valName = r.S("(*$0)", valName)
	}

	// 2. Разбираем суть типа элемента слайса
	switch t := elemType.Underlying().(type) {
	case *types.Basic:
		// Обычные примитивы внутри слайса ([]string, []int)
		if err := g.genBasicValueInline(r, valName, t.Kind()); err != nil {
			return err
		}

	case *types.Struct:
		// ТОТАЛЬНЫЙ ИНЛАЙН СТРУКТУРЫ ВНУТРИ СЛАЙСА (Плевать на *types.Named)
		var fieldsCount int
		type fMeta struct {
			goName  string
			msgName string
			t       types.Type
		}
		var validFields []fMeta

		// Собираем экспортируемые и валидные поля структуры
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if !f.Exported() {
				continue
			}
			tagData := reflect.StructTag(t.Tag(i))
			msgName := tagData.Get(msgpTag)
			if msgName == "-" {
				continue
			}
			if msgName == "" {
				msgName = f.Name()
			}

			fieldsCount++
			validFields = append(validFields, fMeta{goName: f.Name(), msgName: msgName, t: f.Type()})
		}

		// Записываем заголовок мапы для структуры, лежащей внутри слайса
		if fieldsCount <= 15 {
			r.L(`            $dst = append($dst, byte(0x80|($0&0x0f)))`, fieldsCount)
		} else if fieldsCount <= 65535 {
			r.L(`            $dst = append($dst, 0xDE, byte($0>>8), byte($0))`, fieldsCount)
		} else {
			r.L(`            $dst = append($dst, 0xDF, byte($0>>24), byte($0>>16), byte($0>>8), byte($0))`, fieldsCount)
		}

		// Последовательно инлайним поля структуры слева направо
		for _, f := range validFields {
			r.N()
			r.L(`            $dst = $msgp.AppendString($dst, $0)`, strconv.Quote(f.msgName))

			// Формируем дочерний аксессор вида v.FieldName или (*v).FieldName через r.S()
			nextAccessor := r.S("$0.$1", valName, f.goName)
			if err := g.genInlineValue(r.Scope(), nextAccessor, f.t); err != nil {
				return err
			}
		}

	case *types.Slice:
		// РЕКУРСИЯ: Слайс слайсов ([][]T) внутри слайса!
		r.L(`            $dst = $msgp.AppendArrayHeader($dst, uint32(len($0)))`, valName)

		innerValName := r.Uniq("subV")
		r.L(`            for _, $0 := range $1 {`, innerValName, valName)

		// Рекурсивно вызываем этот же метод для вложенного слайса
		if err := g.genSliceElement(r.Scope(), innerValName, t.Elem()); err != nil {
			return err
		}
		r.L(`            }`)

	default:
		g.errorf(token.NoPos, "unsupported slice element type: %s", elemType.String())
		return errors.Newf("unsupported slice element type: %s", elemType.String())
	}

	if isPointer {
		r = r.Parent() // Твой жесткий паникующий .Parent()
		r.L(`        }`)
	}

	return nil
}

func (g *generator) genMapElement(r *goRenderer, valName string, elemType types.Type) error {
	// 1. Обрабатываем случай, если значения мапы — указатели (например, map[string]*int)
	isPointer := false
	if ptr, ok := elemType.(*types.Pointer); ok {
		isPointer = true
		elemType = ptr.Elem()
	}

	if isPointer {
		r.L(`            if $0 == nil {`, valName)
		r.L(`                $dst = $msgp.AppendNil($dst)`)
		r.L(`            } else {`)
		r = r.Scope()
		valName = r.S("(*$0)", valName) // Разименовываем для безопасного доступа
	}

	// 2. Разбираем суть типа значения мапы
	switch t := elemType.Underlying().(type) {
	case *types.Basic:
		// Обычные примитивы внутри мапы (map[string]string, map[string]int)
		if err := g.genBasicValueInline(r, valName, t.Kind()); err != nil {
			return err
		}

	case *types.Struct:
		// ТОТАЛЬНЫЙ ИНЛАЙН СТРУКТУРЫ КАК ЗНАЧЕНИЯ МАПЫ (map[string]MyStruct)
		var fieldsCount int
		type fMeta struct {
			goName  string
			msgName string
			t       types.Type
		}
		var validFields []fMeta

		// Сбор валидных полей структуры
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if !f.Exported() {
				continue
			}

			tagData := reflect.StructTag(t.Tag(i))
			msgName := tagData.Get(msgpTag)
			if msgName == "-" {
				continue
			}
			if msgName == "" {
				msgName = f.Name()
			}

			fieldsCount++
			validFields = append(validFields, fMeta{goName: f.Name(), msgName: msgName, t: f.Type()})
		}

		// Записываем заголовок вложенной мапы для структуры
		if fieldsCount <= 15 {
			r.L(`                $dst = append($dst, byte(0x80|($0&0x0f)))`, fieldsCount)
		} else if fieldsCount <= 65535 {
			r.L(`                $dst = append($dst, 0xDE, byte($0>>8), byte($0))`, fieldsCount)
		} else {
			r.L(`                $dst = append($dst, 0xDF, byte($0>>24), byte($0>>16), byte($0>>8), byte($0))`, fieldsCount)
		}

		// Инлайним поля структуры
		for _, f := range validFields {
			r.N()
			r.L(`                $dst = $msgp.AppendString($dst, $0)`, strconv.Quote(f.msgName))

			// Собираем дочерний аксессор вида v.FieldName через твой рабочий r.S()
			nextAccessor := r.S("$0.$1", valName, f.goName)
			if err := g.genInlineValue(r.Scope(), nextAccessor, f.t); err != nil {
				return err
			}
		}

	case *types.Slice:
		// Значение мапы — это слайс (map[string][]int)
		r.L(`                $dst = $msgp.AppendArrayHeader($dst, uint32(len($0)))`, valName)
		innerValName := r.Uniq("subV")
		r.L(`                for _, $0 := range $1 {`, innerValName, valName)
		if err := g.genSliceElement(r.Scope(), innerValName, t.Elem()); err != nil {
			return err
		}
		r.L(`                }`)

	case *types.Map:
		// Рекурсивная мапа мап (map[string]map[string]int)
		// Нам просто нужно вызвать genInlineValue, передав текущее имя переменной значения!
		if err := g.genInlineValue(r.Scope(), valName, t); err != nil {
			return err
		}

	default:
		g.errorf(token.NoPos, "unsupported map value type: %s", elemType.String())
		return errors.Newf("unsupported map value type: %s", elemType.String())
	}

	if isPointer {
		r = r.Parent() // Твой жесткий паникующий откат скоупа
		r.L(`            }`)
	}

	return nil
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
