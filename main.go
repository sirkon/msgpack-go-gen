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
		kong.Description(
			"A tool for generating msgpack decoders for given structures in a given package.\n"+
				"Struct policies are either StructName or StructName:**, where * is either + or -. StructName:+- \n"+
				"for example.",
		),
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
			Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps |
				packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
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

	plan := map[string]*typeProcessorPlan{}

	for _, structPolicy := range cli.Structs {
		typeDesc := pkg.Types.Scope().Lookup(structPolicy.Name)
		if typeDesc == nil {
			return errors.Newf("type %s not found in the package", structPolicy)
		}

		nt, ok := typeDesc.Type().(*types.Named)
		if !ok {
			return errors.Newf("type %s is not a structure", structPolicy)
		}

		_, ok = nt.Underlying().(*types.Struct)
		if !ok {
			return errors.Newf("type %s is not a structure", structPolicy)
		}
		plan[structPolicy.Name] = &typeProcessorPlan{
			typ:       nt,
			marshal:   structPolicy.Marshal,
			unmarshal: structPolicy.Unmarshal,
		}
	}

	m, err := gogh.New[*importer](
		gogh.GoFmt,
		func(imp *gogh.Imports) *importer {
			return &importer{
				imp: imp,
			}
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
	structNames := slices.Collect(maps.Keys(plan))
	sort.Strings(structNames)

	g := newGenerator(fset, pkg)

	// Per destination file we keep a pair of renderers: pub holds the public
	// methods, funcs the private free functions. pub references a block placed
	// before funcs (via Z) so all methods are rendered before the free
	// functions regardless of the order they are produced.
	type fileStreams struct {
		pub   *goRenderer
		funcs *goRenderer
	}
	files := map[string]*fileStreams{}

	for _, structName := range structNames {
		structPlan := plan[structName]

		position := fset.Position(structPlan.typ.Obj().Pos())
		_, fileName := filepath.Split(position.Filename)
		if path.Ext(fileName) != ".go" {
			return errors.New("we want Go file extension to be .go, got " + path.Ext(position.Filename))
		}

		destName := strings.TrimRight(fileName, ".go") + "_gen.go"

		fs, ok := files[destName]
		if !ok {
			r := p.Go(destName, gogh.Autogen(appName))
			fs = &fileStreams{funcs: r, pub: r.Z()}
			files[destName] = fs
		}

		g.pub = fs.pub
		g.funcs = fs.funcs

		if structPlan.marshal {
			if err := g.genMarshaler(fs.pub, structName, structPlan.typ); err != nil {
				return errors.Wrap(err, "generate marshaler for "+structName)
			}
		}
		if structPlan.unmarshal {
			if err := g.genUnmarshaler(fs.pub, structName, structPlan.typ); err != nil {
				return errors.Wrap(err, "generate unmarshaler for "+structName)
			}
		}
	}

	if err := m.Render(); err != nil {
		return errors.Wrap(err, "render generated code")
	}

	return nil
}

type typeProcessorPlan struct {
	typ       *types.Named
	marshal   bool
	unmarshal bool
}
