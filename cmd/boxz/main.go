package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dnswlt/boxz"
)

func main() {
	os.Exit(run())
}

func run() int {
	outputPath := flag.String("o", "-", "output SVG file (default: stdout)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: boxz [-o output.svg] input.boxz\n\n")
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
	if err := boxz.RenderSVG(output, doc, boxz.DefaultConfig()); err != nil {
		fmt.Fprintf(os.Stderr, "boxz: %v\n", err)
		return 1
	}
	return 0
}
