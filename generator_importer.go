package main

import (
	"github.com/sirkon/gogh"
)

type importer struct {
	imp *gogh.Imports
}

func (i *importer) Add(pkgpath string) *gogh.ImportAliasControl {
	return i.imp.Add(pkgpath)
}

func (i *importer) Module(relpath string) *gogh.ImportAliasControl {
	return i.imp.Module(relpath)
}

func (i *importer) Msgp() *gogh.ImportAliasControl {
	return i.imp.Add("github.com/tinylib/msgp/msgp")
}

func (i *importer) Imports() *gogh.Imports {
	return i.imp.Imports()
}
