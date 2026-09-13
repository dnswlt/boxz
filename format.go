package boxz

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Format writes the canonical boxz source for doc. Comments and incidental
// whitespace are intentionally not retained; element and edge order are.
func Format(w io.Writer, doc *Document) error {
	if err := doc.checkInitialized(); err != nil {
		return err
	}
	var output strings.Builder
	formatElement(&output, doc.root, 0)
	if len(doc.edges) != 0 {
		output.WriteString("\nedges {\n")
		for _, edge := range doc.edges {
			output.WriteString("  ")
			formatEndpoint(&output, edge.From, edge.FromSide)
			output.WriteString(" -> ")
			formatEndpoint(&output, edge.To, edge.ToSide)
			output.WriteByte('\n')
		}
		output.WriteString("}\n")
	}
	_, err := io.WriteString(w, output.String())
	return err
}

func formatElement(output *strings.Builder, element *element, depth int) {
	indent(output, depth)
	fmt.Fprintf(output, "%s %s", element.kind, formatID(element.ID))
	if element.kind == kindNode {
		if element.Title != "" && element.Title != element.ID {
			fmt.Fprintf(output, " %s", strconv.Quote(element.Title))
		}
	} else if element.Title != "" {
		fmt.Fprintf(output, " %s", strconv.Quote(element.Title))
	}
	formatAttributes(output, element)
	if element.kind == kindNode {
		output.WriteByte('\n')
		return
	}

	output.WriteString(" {\n")
	for index, child := range element.Children {
		formatSprings(output, formatSpringCount(element, index), depth+1)
		formatElement(output, child, depth+1)
	}
	formatSprings(output, formatSpringCount(element, len(element.Children)), depth+1)
	indent(output, depth)
	output.WriteString("}\n")
}

func formatAttributes(output *strings.Builder, element *element) {
	var attributes []string
	if element.kind == kindNode {
		if element.nodeAttributes.Spring {
			attributes = append(attributes, "spring")
		}
	} else {
		alignment := element.containerAttributes.LabelAlign
		if alignment != "" && alignment != labelAlignAuto {
			attributes = append(attributes, "labelAlign = "+string(alignment))
		}
		defaultBounded := element.Title != ""
		if element.containerAttributes.Bounded != defaultBounded {
			if element.containerAttributes.Bounded {
				attributes = append(attributes, "bounded")
			} else {
				attributes = append(attributes, "bounded = false")
			}
		}
	}
	if len(attributes) != 0 {
		fmt.Fprintf(output, " [%s]", strings.Join(attributes, ", "))
	}
}

func formatSprings(output *strings.Builder, count, depth int) {
	for range count {
		indent(output, depth)
		output.WriteString("spring\n")
	}
}

func formatSpringCount(element *element, index int) int {
	if index < len(element.Springs) {
		return element.Springs[index]
	}
	return 0
}

func formatEndpoint(output *strings.Builder, id string, side *Side) {
	output.WriteString(formatID(id))
	if side != nil {
		output.WriteByte(':')
		output.WriteString(string(*side))
	}
}

func formatID(id string) string {
	if isPlainIdentifier(id) {
		return id
	}
	return "`" + id + "`"
}

func indent(output *strings.Builder, depth int) {
	output.WriteString(strings.Repeat("  ", depth))
}
