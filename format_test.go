package boxz

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatCanonicalSource(t *testing.T) {
	doc, err := ParseString("format.boxz", `
vbox `+"`system:core`"+` "System" [bounded = false, labelAlign = right] {
  spring spring
  node `+"`api-server`"+` "API" [spring = true,]
  hbox services [bounded] { node db }
}
edges { `+"`api-server`"+`:S -> db:N }
`)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Format(&output, doc); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"vbox `system:core` \"System\" [labelAlign = right, bounded = false] {\n" +
		"  spring\n" +
		"  spring\n" +
		"  node `api-server` \"API\" [spring]\n" +
		"  hbox services [bounded] {\n" +
		"    node db\n" +
		"  }\n" +
		"}\n" +
		"\n" +
		"edges {\n" +
		"  `api-server`:S -> db:N\n" +
		"}\n"
	if got := output.String(); got != want {
		t.Fatalf("Format output:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatPreservesRenderingAndIsIdempotentForEveryExample(t *testing.T) {
	err := filepath.WalkDir("examples", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".boxz" {
			return nil
		}
		t.Run(strings.TrimSuffix(filepath.ToSlash(path), ".boxz"), func(t *testing.T) {
			source, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			doc, err := Parse(path, source)
			if err != nil {
				t.Fatal(err)
			}
			var originalSVG bytes.Buffer
			if err := RenderSVG(&originalSVG, doc); err != nil {
				t.Fatal(err)
			}
			var first bytes.Buffer
			if err := Format(&first, doc); err != nil {
				t.Fatal(err)
			}
			reparsed, err := ParseString(path, first.String())
			if err != nil {
				t.Fatalf("Parse(Format(doc)): %v\n%s", err, first.String())
			}
			var reparsedSVG bytes.Buffer
			if err := RenderSVG(&reparsedSVG, reparsed); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(originalSVG.Bytes(), reparsedSVG.Bytes()) {
				t.Fatal("Parse(Format(doc)) changed rendered SVG")
			}
			var second bytes.Buffer
			if err := Format(&second, reparsed); err != nil {
				t.Fatal(err)
			}
			if first.String() != second.String() {
				t.Fatalf("format is not idempotent:\nfirst:\n%s\nsecond:\n%s", first.String(), second.String())
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
