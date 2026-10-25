package office

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
)

// DocxText returns the text of a Word document: paragraphs as lines, tables as rows
// like "| cell | cell |". Headers, footers, footnotes and text deleted in track changes
// are not included.
func DocxText(data []byte) (string, error) {
	arc, err := openArchive(data)
	if err != nil {
		return "", err
	}
	document, err := arc.mustRead("word/document.xml")
	if err != nil {
		return "", err
	}
	return parseDocument(document)
}

type docxParser struct {
	out       strings.Builder
	paragraph strings.Builder
	cells     [][]string // stack of table rows: cells of the current row at each nesting level
	cell      []*strings.Builder
	inText    bool
	// runDepth > 0 inside a text run (w:r). w:tab also appears in paragraph
	// properties (tab stops), where it does not mean a tab character.
	runDepth int
}

func parseDocument(document []byte) (string, error) {
	decoder, err := newDecoder(document)
	if err != nil {
		return "", partError("word/document.xml", err)
	}
	p := &docxParser{}

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", partError("word/document.xml", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			p.start(t.Name.Local)
		case xml.EndElement:
			p.end(t.Name.Local)
		case xml.CharData:
			if p.inText {
				p.paragraph.Write(t)
			}
		}
	}

	return strings.TrimSpace(p.out.String()), nil
}

func (p *docxParser) start(name string) {
	switch name {
	case "r":
		p.runDepth++
	case "t":
		p.inText = true
	case "tab":
		if p.runDepth > 0 {
			p.paragraph.WriteByte('\t')
		}
	case "br", "cr":
		if p.runDepth > 0 {
			p.paragraph.WriteByte('\n')
		}
	case "tr":
		p.cells = append(p.cells, nil)
	case "tc":
		p.cell = append(p.cell, &strings.Builder{})
	}
}

func (p *docxParser) end(name string) {
	switch name {
	case "r":
		p.runDepth = max(0, p.runDepth-1)
	case "t":
		p.inText = false
	case "p":
		p.flushParagraph()
	case "tc":
		if len(p.cell) == 0 {
			return
		}
		text := strings.TrimSpace(p.cell[len(p.cell)-1].String())
		p.cell = p.cell[:len(p.cell)-1]
		if len(p.cells) == 0 {
			return
		}
		row := len(p.cells) - 1
		p.cells[row] = append(p.cells[row], text)
	case "tr":
		if len(p.cells) == 0 {
			return
		}
		row := p.cells[len(p.cells)-1]
		p.cells = p.cells[:len(p.cells)-1]
		line := "| " + strings.Join(row, " | ") + " |"
		if len(p.cell) > 0 {
			// Nested table: the row becomes part of a cell of the outer table.
			appendSeparated(p.cell[len(p.cell)-1], line)
			return
		}
		p.out.WriteString(line + "\n")
	case "tbl":
		if len(p.cell) == 0 {
			p.out.WriteByte('\n')
		}
	}
}

func (p *docxParser) flushParagraph() {
	text := strings.TrimRight(p.paragraph.String(), " \t")
	p.paragraph.Reset()

	if len(p.cell) > 0 {
		// Paragraphs inside a cell are joined into one line so the table does not break.
		appendSeparated(p.cell[len(p.cell)-1], strings.ReplaceAll(strings.TrimSpace(text), "\n", " "))
		return
	}
	p.out.WriteString(text + "\n")
}

func appendSeparated(b *strings.Builder, text string) {
	if text == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteString(" / ")
	}
	b.WriteString(text)
}
