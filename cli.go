package main

const appName = "msgpack-go-gen"

type CLI struct {
	Package string   `short:"p" help:"Package directory path." default:"." required:""`
	Structs []string `arg:"" required:"" help:"Structs to generate encoders for."`
}
