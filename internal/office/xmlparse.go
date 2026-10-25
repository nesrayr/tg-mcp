package office

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	maxXMLDepth = 128
	maxTagBytes = 64 << 10
)

var (
	errTooDeep    = fmt.Errorf("XML nested deeper than %d levels", maxXMLDepth)
	errTagTooLong = fmt.Errorf("XML tag longer than %d bytes", maxTagBytes)
)

var unparsedBlocks = [][2]string{
	{"<!--", "-->"},
	{"<![CDATA[", "]]>"},
	{"<?", "?>"},
}

type visitFunc func(d *xml.Decoder, start xml.StartElement, parent string) (consumed bool, err error)

func newDecoder(data []byte) (*xml.Decoder, error) {
	// encoding/xml keeps all attributes of a tag and an entry per open element
	// without limits, so a few kilobytes of compressed XML could take gigabytes.
	if err := checkTagSize(data); err != nil {
		return nil, err
	}
	return xml.NewTokenDecoder(&depthLimiter{raw: xml.NewDecoder(bytes.NewReader(data))}), nil
}

type depthLimiter struct {
	raw   *xml.Decoder
	depth int
}

func (l *depthLimiter) Token() (xml.Token, error) {
	token, err := l.raw.RawToken()
	switch token.(type) {
	case xml.StartElement:
		l.depth++
		if l.depth > maxXMLDepth {
			return nil, errTooDeep
		}
	case xml.EndElement:
		l.depth--
	}
	return token, err
}

func checkTagSize(data []byte) error {
	for i := 0; i < len(data); {
		open := bytes.IndexByte(data[i:], '<')
		if open < 0 {
			return nil
		}
		i += open

		skipped := false
		for _, block := range unparsedBlocks {
			open, end := []byte(block[0]), []byte(block[1])
			if !bytes.HasPrefix(data[i:], open) {
				continue
			}
			length := bytes.Index(data[i+len(open):], end)
			if length < 0 {
				return nil
			}
			i += len(open) + length + len(end)
			skipped = true
			break
		}
		if skipped {
			continue
		}

		end := tagEnd(data[i:])
		if end > maxTagBytes {
			return errTagTooLong
		}
		if end < 0 {
			return nil
		}
		i += end + 1
	}
	return nil
}

func tagEnd(tag []byte) int {
	var quote byte
	for i := 1; i < len(tag); i++ {
		switch c := tag[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i
		}
		if i > maxTagBytes {
			return i
		}
	}
	return -1
}

func walk(data []byte, part string, visit visitFunc) error {
	d, err := newDecoder(data)
	if err != nil {
		return partError(part, err)
	}

	var path []string
	for {
		token, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return partError(part, err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			parent := ""
			if len(path) > 0 {
				parent = path[len(path)-1]
			}
			consumed, visitErr := visit(d, t, parent)
			if visitErr != nil {
				return partError(part, visitErr)
			}
			if !consumed {
				path = append(path, t.Name.Local)
			}
		case xml.EndElement:
			path = path[:len(path)-1]
		}
	}
}

func partError(part string, err error) error {
	if errors.Is(err, ErrTooLarge) {
		return err
	}
	return fmt.Errorf("%w: %q: %v", ErrInvalidDocument, part, err)
}

func attr(start xml.StartElement, space, local string) string {
	for _, a := range start.Attr {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func readValue(d *xml.Decoder) (string, error) {
	var b strings.Builder
	for {
		token, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			err = d.Skip()
		case xml.EndElement:
			return b.String(), nil
		}
		if err != nil {
			return "", err
		}
	}
}

func readRichText(d *xml.Decoder) (string, error) {
	// A string is either a plain <t> or a set of <r><t> runs; phonetic hints (<rPh>) are skipped.
	var b strings.Builder
	depth := 0
	for {
		token, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				var text string
				text, err = readValue(d)
				b.WriteString(text)
			case "r":
				depth++
			default:
				err = d.Skip()
			}
		case xml.EndElement:
			if depth == 0 {
				return b.String(), nil
			}
			depth--
		}
		if err != nil {
			return "", err
		}
	}
}
