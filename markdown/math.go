package mark

import (
	"bytes"
	"strings"

	"github.com/kovetskiy/mark/v16/renderer"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// MathInline represents an inline LaTeX math formula.
type MathInline struct {
	ast.BaseInline
	Formula string
}

func (n *MathInline) Kind() ast.NodeKind {
	return renderer.MathInlineKind
}

func (n *MathInline) GetFormula() string {
	return n.Formula
}

func (n *MathInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Formula": n.Formula}, nil)
}

// MathBlock represents a block LaTeX math formula.
type MathBlock struct {
	ast.BaseBlock
	Formula string
}

func (n *MathBlock) Kind() ast.NodeKind {
	return renderer.MathBlockKind
}

func (n *MathBlock) GetFormula() string {
	return n.Formula
}

func (n *MathBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Formula": n.Formula}, nil)
}

// MathInlineParser parses inline math expressions ($...$ or \(...\)).
type MathInlineParser struct{}

func NewMathInlineParser() parser.InlineParser {
	return &MathInlineParser{}
}

func (p *MathInlineParser) Trigger() []byte {
	return []byte{'$', '\\'}
}

func (p *MathInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) == 0 {
		return nil
	}

	if line[0] == '$' {
		if len(line) > 1 && line[1] == '$' {
			return nil // Let MathBlockParser handle $$
		}
		endIdx := bytes.IndexByte(line[1:], '$')
		if endIdx <= 0 {
			return nil
		}
		endIdx += 1

		formula := strings.TrimSpace(string(line[1:endIdx]))
		if formula == "" {
			return nil
		}

		block.Advance(endIdx + 1)
		return &MathInline{Formula: formula}
	}

	if len(line) >= 2 && line[0] == '\\' && line[1] == '(' {
		endIdx := bytes.Index(line[2:], []byte("\\)"))
		if endIdx < 0 {
			return nil
		}
		endIdx += 2

		formula := strings.TrimSpace(string(line[2:endIdx]))
		if formula == "" {
			return nil
		}

		block.Advance(endIdx + 2)
		return &MathInline{Formula: formula}
	}

	return nil
}

// MathBlockParser parses display math blocks ($$...$$ or \[...\]).
type MathBlockParser struct{}

func NewMathBlockParser() parser.BlockParser {
	return &MathBlockParser{}
}

func (p *MathBlockParser) Trigger() []byte {
	return []byte{'$', '\\'}
}

func (p *MathBlockParser) CanInterruptParagraph() bool {
	return true
}

func (p *MathBlockParser) CanAcceptIndentedLine() bool {
	return false
}

func (p *MathBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	trimmed := bytes.TrimSpace(line)

	if bytes.HasPrefix(trimmed, []byte("$$")) {
		content := trimmed[2:]
		if bytes.HasSuffix(content, []byte("$$")) && len(content) >= 2 {
			formula := strings.TrimSpace(string(content[:len(content)-2]))
			reader.Advance(segment.Len())
			return &MathBlock{Formula: formula}, parser.Close
		}
		return &MathBlock{}, parser.NoChildren
	}

	if bytes.HasPrefix(trimmed, []byte("\\[")) {
		content := trimmed[2:]
		if bytes.HasSuffix(content, []byte("\\]")) && len(content) >= 2 {
			formula := strings.TrimSpace(string(content[:len(content)-2]))
			reader.Advance(segment.Len())
			return &MathBlock{Formula: formula}, parser.Close
		}
		return &MathBlock{}, parser.NoChildren
	}

	return nil, parser.None
}

func (p *MathBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, segment := reader.PeekLine()
	trimmed := bytes.TrimSpace(line)
	mathBlock := node.(*MathBlock)

	if mathBlock.Formula == "" && node.Lines().Len() == 0 {
		if bytes.Equal(trimmed, []byte("$$")) || bytes.Equal(trimmed, []byte("\\[")) {
			reader.Advance(segment.Len())
			return parser.NoChildren
		}
	}

	if bytes.Equal(trimmed, []byte("$$")) || bytes.Equal(trimmed, []byte("\\]")) {
		reader.Advance(segment.Len())
		return parser.Close
	}

	node.Lines().Append(segment)
	if mathBlock.Formula != "" {
		mathBlock.Formula += "\n"
	}
	mathBlock.Formula += string(bytes.TrimRight(line, "\r\n"))
	reader.Advance(segment.Len())
	return parser.Continue
}

func (p *MathBlockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	mathBlock := node.(*MathBlock)
	mathBlock.Formula = strings.TrimSpace(mathBlock.Formula)
}
