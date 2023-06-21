package parser

import (
	"bytes"
	"regexp"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var blockTagnamePattern = `([A-Za-z][A-Za-z0-9-]*)`

var blockAttributePattern = `(?:[\r\n \t]+[a-zA-Z_:][a-zA-Z0-9:._-]*(?:[\r\n \t]*=[\r\n \t]*(?:[^\"'=<>` + "`" + `\x00-\x20]+|'[^']*'|"[^"]*"))?)`

// Only match <ac:*> and <ri:*> tags
var blockOpenTagRegexp = regexp.MustCompile("^<(ac|ri):" + blockTagnamePattern + blockAttributePattern + `*[ \t]*>`)
var blockCloseTagRegexp = regexp.MustCompile("^</ac:" + blockTagnamePattern + `\s*>`)

// NewConfluenceTagParser returns an block parser that parses <ac:*> and <ri:*> tags to ensure that Confluence specific tags are parsed
// as ast.KindHTMLBlock so they are not escaped at render time. The parser must be registered with a higher priority
// than goldmark's linkParser. Otherwise, the linkParser would parse the <ac:* /> tags.
func NewConfluenceTagBlockParser() parser.BlockParser {
	return &confluenceTagBlockParser{}
}

var _ parser.BlockParser = (*confluenceTagBlockParser)(nil)

// confluenceTagParser is a stripped down version of goldmark's rawHTMLParser.
// See: https://github.com/yuin/goldmark/blob/master/parser/raw_html.go
type confluenceTagBlockParser struct {
}

func (s *confluenceTagBlockParser) Trigger() []byte {
	return []byte{'<'}
}

func (b *confluenceTagBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	var node *ast.HTMLBlock
	line, segment := reader.PeekLine()

	if m := blockOpenTagRegexp.FindSubmatchIndex(line); m != nil {
		node = ast.NewHTMLBlock(ast.HTMLBlockType1)
	}
	if node != nil {
		reader.Advance(segment.Len() - util.TrimRightSpaceLength(line))
		node.Lines().Append(segment)
		return node, parser.HasChildren
	}
	return nil, parser.NoChildren
}

func (b *confluenceTagBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	htmlBlock := node.(*ast.HTMLBlock)
	lines := htmlBlock.Lines()
	line, segment := reader.PeekLine()
	var closurePattern []byte

	if htmlBlock.HTMLBlockType == ast.HTMLBlockType1 {
		if lines.Len() == 1 {
			firstLine := lines.At(0)
			if blockCloseTagRegexp.Match(firstLine.Value(reader.Source())) {
				return parser.Close
			}
		}
		if blockCloseTagRegexp.Match(line) {
			htmlBlock.ClosureLine = segment
			reader.Advance(segment.Len() - util.TrimRightSpaceLength(line))
			return parser.Close
		}

		if lines.Len() == 1 {
			firstLine := lines.At(0)
			if bytes.Contains(firstLine.Value(reader.Source()), closurePattern) {
				return parser.Close
			}
		}
		if bytes.Contains(line, closurePattern) {
			htmlBlock.ClosureLine = segment
			reader.Advance(segment.Len())
			return parser.Close
		}
	}
	node.Lines().Append(segment)
	reader.Advance(segment.Len() - util.TrimRightSpaceLength(line))
	return parser.Continue | parser.HasChildren
}

func (b *confluenceTagBlockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	// nothing to do
}

func (b *confluenceTagBlockParser) CanInterruptParagraph() bool {
	return true
}

func (b *confluenceTagBlockParser) CanAcceptIndentedLine() bool {
	return true
}
