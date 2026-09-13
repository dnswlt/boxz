package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dnswlt/boxz"
	"github.com/dnswlt/boxz/internal/renderconfig"
)

func main() {
	os.Exit(run())
}

func run() int {
	outputPath := flag.String("o", "-", "output SVG file (default: stdout)")
	debug := flag.Bool("debug", false, "draw containers and routing diagnostics")
	router := flag.String("router", "builtin", "edge router: builtin or avoid (experimental)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: boxz [-debug] [-router builtin|avoid] [-o output.svg] input.boxz\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		return 2
	}

	inputPath := flag.Arg(0)
	input, err := os.Open(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "boxz: %v\n", err)
		return 1
	}
	defer input.Close()

	doc, err := boxz.Parse(inputPath, input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	var output io.Writer = os.Stdout
	var file *os.File
	if *outputPath != "-" {
		file, err = os.Create(*outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "boxz: %v\n", err)
			return 1
		}
		defer file.Close()
		output = file
	}
	cfg := renderconfig.Default()
	cfg.Debug = *debug
	switch *router {
	case "builtin":
		cfg.EdgeRouter = renderconfig.RouterBuiltin
	case "avoid":
		cfg.EdgeRouter = renderconfig.RouterAvoid
	default:
		fmt.Fprintf(os.Stderr, "boxz: unknown router %q; want builtin or avoid\n", *router)
		return 2
	}
	if err := boxz.RenderSVG(output, doc, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "boxz: %v\n", err)
		return 1
	}
	return 0
}
