package main

import (
	"go/token"
	"go/types"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/sirkon/errors"
	"github.com/sirkon/gogh"
	"github.com/sirkon/jsonexec"
	"github.com/sirkon/message"
	"golang.org/x/tools/go/packages"
)

func main() {
	var cli CLI

	parser := kong.Parse(&cli,
		kong.Name(appName),
		kong.Description("A tool for generating msgpack decoders for given structures in a given package."),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	)

	if _, err := parser.Parse(os.Args[1:]); err != nil {
		parser.FatalIfErrorf(err)
	}

	if err := job(&cli); err != nil {
		message.Fatal(err)
	}
}

func job(cli *CLI) error {
	if err := os.Chdir(cli.Package); err != nil {
		return errors.Wrap(err, "chdir into package directory "+cli.Package)
	}

	var listOutput struct {
		Dir        string
		ImportPath string
	}
	if err := jsonexec.Run(&listOutput, "go", "list", "-json"); err != nil {
		return errors.Wrap(err, "run go list to compute import path of the given package")
	}

	message.Infof("--- package %s (in %s)", listOutput.ImportPath, listOutput.Dir)

	fset := token.NewFileSet()
	pkgs, err := packages.Load(
		&packages.Config{
			Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps | packages.NeedTypes | packages.NeedTypesInfo,
			Fset: fset,
		},
		listOutput.Dir,
	)
	if err != nil {
		return errors.Wrap(err, "load package")
	}

	switch len(pkgs) {
	case 0:
		return errors.New("no packages found")
	case 1:
	default:
		return errors.New("multiple packages detected instead of just one")
	}

	pkg := pkgs[0]

	structs := map[string]*types.Named{}
	for _, typeName := range cli.Structs {
		typeDesc := pkg.Types.Scope().Lookup(typeName)
		if typeDesc == nil {
			return errors.Newf("type %s not found in the package", typeName)
		}

		nt, ok := typeDesc.Type().(*types.Named)
		if !ok {
			return errors.Newf("type %s is not a structure", typeName)
		}

		_, ok = nt.Underlying().(*types.Struct)
		if !ok {
			return errors.Newf("type %s is not a structure", typeName)
		}

		structs[typeName] = nt
	}

	m, err := gogh.New[*gogh.Imports](
		gogh.GoFmt,
		func(r *gogh.Imports) *gogh.Imports {
			return r
		},
	)
	if err != nil {
		return errors.Wrap(err, "initialize code renderer")
	}

	p, err := m.Current(pkg.Name)
	if err != nil {
		return errors.Wrap(err, "initialize current package renderer")
	}

	// Order struct names alphabetically for the sake of perceptible stability.
	structNames := slices.Collect(maps.Keys(structs))
	sort.Strings(structNames)
	for _, structName := range structNames {
		typeDesc := structs[structName]

		position := fset.Position(typeDesc.Obj().Pos())
		_, fileName := filepath.Split(position.Filename)
		if path.Ext(fileName) != ".go" {
			return errors.New("we want Go file extension to be .go, got " + path.Ext(position.Filename))
		}

		destName := strings.TrimRight(fileName, ".go") + "_gen.go"

		r := p.Go(destName, gogh.Autogen(appName))
		g := &generator{
			fset: fset,
		}
		if err := g.generateMarshaler(r, structName, typeDesc); err != nil {
			return errors.Wrap(err, "generate marshaler for "+structName)
		}
	}

	if err := m.Render(); err != nil {
		return errors.Wrap(err, "render generated code")
	}

	return nil
}
