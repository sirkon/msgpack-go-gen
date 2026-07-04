package main

type fieldCountCorrectMethod interface {
	isFieldCountCorrectMethod()
}

type fieldCountCorrectMethodOffset struct {
	offset int
}

type fieldCountCorrectMethodChange struct{}

func (f *fieldCountCorrectMethodOffset) isFieldCountCorrectMethod() {}
func (f *fieldCountCorrectMethodChange) isFieldCountCorrectMethod() {}
