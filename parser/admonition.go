// The admonition parser started as github.com/stefanfritsch/goldmark-admonitions
// v1.1.1, which is no longer maintained:
//
// Copyright (c) 2019 Yusuke Inuzuka
// Copyright (c) 2022 Stefan Fritsch
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package parser

import (
	"bytes"
	"slices"
	"strings"

	"github.com/kovetskiy/mark/v17/internal/htmlblock"
	"github.com/kovetskiy/mark/v17/internal/parsenote"
	"github.com/kovetskiy/mark/v17/internal/syntax"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// Admonition is a MkDocs admonition block:
//
//	!!! note "Title" {.class #id key=value}
//	    Body, indented under the opening line.
//
// A body level with the opening line is one only up to a closing run:
//
//	!!! note "Title"
//	Body.
//	!!!
//
// Without the run the admonition ends at its opening line.
type Admonition struct {
	ast.BaseBlock

	// AdmonitionClass is the word after the opening run, "note" above.
	AdmonitionClass []byte

	// Title is the rest of the opening line before any attributes, quotes
	// included.
	Title []byte

	// lookAhead is what was read ahead about the closing run, for the tests
	// that hold it to the parse.
	lookAhead lookAhead
}

// lookAhead is what the parser read ahead about one admonition's closing run,
// and what came of it.
type lookAhead struct {
	// asked is whether the body started level with the opening line, so that
	// a closing run was looked for.
	asked bool

	// indent is the indentation of the opening line.
	indent int

	// runEnd is where the run that was found ends, and -1 when none was.
	runEnd int

	// closedAt is where the run that closed the admonition ends, and 0 for
	// one that no run closed.
	closedAt int
}

// Dump implements ast.Node.
func (n *Admonition) Dump(_ []byte) *ast.NodeDump {
	return ast.NewNodeDump(n, map[string]any{
		"AdmonitionClass": string(n.AdmonitionClass),
		"Title":           string(n.Title),
	})
}

// KindAdmonition is the ast.NodeKind of an Admonition.
var KindAdmonition = ast.NewNodeKind("Admonition")

// Kind implements ast.Node.
func (n *Admonition) Kind() ast.NodeKind {
	return KindAdmonition
}

// NewAdmonition returns an empty Admonition.
func NewAdmonition() *Admonition {
	n := &Admonition{}
	n.Init(n)
	return n
}

type admonitionParser struct{}

// NewAdmonitionParser returns a block parser for MkDocs admonitions.
func NewAdmonitionParser() parser.BlockParser {
	return &admonitionParser{}
}

// admonitionState is what the parser keeps about one open admonition between
// lines.
type admonitionState struct {
	node *Admonition

	// indent is the indentation of the opening line, and so of the closing one.
	indent int

	// length is the number of '!' that opened it; a closing run has to be at
	// least as long.
	length int

	// contentIndent is the indentation of the first non-blank line of the
	// body, which every later body line is read relative to.
	contentIndent     int
	contentHasStarted bool

	// gap is whether a blank line came between the opening line and the
	// body.
	gap bool

	// runEnd is where the closing run of a body level with the opening line
	// ends, as the look-ahead found it at the first body line, and -1 for
	// any other body.
	runEnd int
}

// indented reports whether the body is indented under the opening line, and
// so ends at the first line that is not.
func (s *admonitionState) indented() bool {
	return s.contentHasStarted && s.contentIndent > s.indent
}

// admonitionStackKey holds the admonitions open at the current line,
// outermost first, as []*admonitionState.
var admonitionStackKey = parser.NewContextKey()

func admonitionStack(pc parser.Context) []*admonitionState {
	stack, _ := pc.Get(admonitionStackKey).([]*admonitionState)
	return stack
}

func (b *admonitionParser) Trigger() []byte {
	return []byte{'!'}
}

func (b *admonitionParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || line[pos] != '!' {
		return nil, parser.NoChildren
	}
	indent := pos

	i := pos
	for ; i < len(line) && line[i] == '!'; i++ {
	}
	length := i - pos
	if length < 3 {
		return nil, parser.NoChildren
	}

	// A run of '!' with nothing after it is not an opening line: there would
	// be no telling whether a later bare run closes this admonition or opens
	// another.
	content := withoutLineEnding(line)
	left := i + util.TrimLeftSpaceLength(content[i:])
	if left >= len(content) {
		return nil, parser.NoChildren
	}

	node := parseAdmonitionOpeningLine(reader, left)

	state := &admonitionState{
		node:   node,
		indent: indent,
		length: length,
		runEnd: -1,
	}
	pc.Set(admonitionStackKey, append(admonitionStack(pc), state))

	line, _ = reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())
	if closing, _ := hasAdmonitionClosingRun(line, w, pos, state); w < state.indent || closing {
		return node, parser.NoChildren
	}

	return node, parser.HasChildren
}

// parseAdmonitionOpeningLine reads the class, title and attributes of an
// opening line, starting left bytes into it.
func parseAdmonitionOpeningLine(reader text.Reader, left int) *Admonition {
	node := NewAdmonition()
	reader.Advance(left)

	remainingLine, _ := reader.PeekLine()
	remainingLine = withoutLineEnding(remainingLine)
	remainingLength := len(remainingLine)

	endClass := 0
	for ; endClass < remainingLength && remainingLine[endClass] != ' ' && remainingLine[endClass] != '{'; endClass++ {
	}
	if endClass > 0 {
		node.AdmonitionClass = remainingLine[0:endClass]
	}

	startTitle := endClass + util.TrimLeftSpaceLength(remainingLine[endClass:])
	endTitle := startTitle
	for ; endTitle < remainingLength && remainingLine[endTitle] != '{'; endTitle++ {
	}
	if endTitle > startTitle {
		endTitle -= util.TrimRightSpaceLength(remainingLine[startTitle:endTitle])
		if endTitle > startTitle {
			node.Title = remainingLine[startTitle:endTitle]
		}
	}

	if endTitle < remainingLength {
		reader.Advance(endTitle)
	} else {
		reader.Advance(remainingLength)
	}

	class := append([]byte("admonition adm-"), node.AdmonitionClass...)
	hasClass := false

	// The block has to end the opening line, in goldmark v1's grammar (see
	// syntax.AttributeBlock): goldmark v2's would take "{x}" and read on into the
	// body's first line for one.
	var attributes []ast.Attribute

	ok := false
	if _, at := reader.Position(); attributesEndLine(reader.Source(), at) {
		attributes, ok = parser.ParseAttributes(reader)
	}

	// ParseAttributes skips blanks before the '{', and a newline is one, so
	// on a line with no attributes it reads into the next line before putting
	// the position back. Putting it back does not drop the line it peeked, and
	// goldmark then takes that line for the rest of this one: inside a
	// blockquote it opened a second quote on the same line and panicked.
	// Advancing by nothing drops the stale line.
	reader.Advance(0)

	if ok {
		for _, attribute := range attributes {
			// goldmark reads every attribute value as text: {width=5} is "5"
			// and {open=true} is "true".
			if !admonitionAttributeIsText(attribute, reader.Source()) {
				continue
			}

			if attribute.Name == "class" {
				hasClass = true
				value := string(class) + " " + attribute.Value.Value(reader.Source())
				node.SetAttribute(attribute.Name, text.NewMultiLineValueFromString(value, text.IdentityDecoder))

				continue
			}

			node.SetAttribute(attribute.Name, attribute.Value)
		}
	}

	if !hasClass {
		node.SetAttribute("class", text.NewMultiLineValueFromString(string(class), text.IdentityDecoder))
	}

	return node
}

// admonitionAttributeIsText reports whether an attribute value is one an HTML
// attribute can carry: an unquoted [1,2], {a=b} or null is a list, an object
// or nothing, written in a syntax an HTML attribute has no spelling for, so it
// is left out.
func admonitionAttributeIsText(attribute ast.Attribute, source []byte) bool {
	value := attribute.Value
	if value.IsOwned() {
		return true
	}

	start := value.Index().Start
	if start > 0 && (source[start-1] == '"' || source[start-1] == '\'') {
		return true
	}

	raw := value.Str(source)

	return raw != "null" && !strings.HasPrefix(raw, "[") && !strings.HasPrefix(raw, "{")
}

func (b *admonitionParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	stack := admonitionStack(pc)
	level := -1
	for i, state := range stack {
		if state.node == node {
			level = i
			break
		}
	}
	if level < 0 {
		return parser.Close
	}
	state := stack[level]

	line, segment := reader.PeekLine()
	w, pos := util.IndentWidth(line, reader.LineOffset())

	first := !state.contentHasStarted && !util.IsBlank(line[pos:])
	if first {
		state.contentHasStarted = true
		state.contentIndent = w
	} else if !state.contentHasStarted {
		state.gap = true
	}

	// An indented body ends at any closing run. One level with the opening
	// line ends at the run the look-ahead found and at no other: the two read
	// the body by different means, and an admonition kept open for a run it
	// then left to a block of its body would run to the end of the document.
	closing, newline := hasAdmonitionClosingRun(line, w, pos, state)
	if closing && (first || state.indented() || segment.Stop == state.runEnd) {
		state.node.lookAhead.closedAt = segment.Stop

		// Len counts the padding already, which Advance steps over first.
		reader.Advance(segment.Len() - newline)

		return parser.Close
	}

	// A body that is not indented is one only up to a closing run. With none
	// ahead the admonition ends here, at its opening line, and this line is
	// read after it.
	if first && w == state.indent {
		state.runEnd = closingRunAhead(reader, pc, state.indent, state.length)
		state.node.lookAhead = lookAhead{asked: true, indent: state.indent, runEnd: state.runEnd}

		if state.runEnd < 0 {
			// Straight after the opening line, the line was written as the
			// admonition's body, and its author is told that it is not. After
			// a blank line it is what follows an admonition with no body, the
			// way MkDocs writes one. Where the opening line left a paragraph
			// of its own in the admonition, the line may go on that one.
			if !state.gap && !node.HasChildren() {
				parsenote.StrandedBody(pc, node.Pos())
			}

			return parser.Close
		}
	}

	// Without a closing run, a line ends the admonition when it is indented
	// less than the opening line, or level with it once the body was indented.
	if !util.IsBlank(line) && (w < state.indent || (w == state.indent && w < state.contentIndent)) {
		return parser.Close
	}

	if state.contentIndent > 0 {
		// Step over the body's indentation, and no further: a lazy line
		// indented less than the body keeps its text, and a tab counts as
		// the columns it spans rather than as one byte.
		if pos, padding := util.IndentPosition(line, reader.LineOffset(), state.contentIndent); pos >= 0 {
			reader.AdvanceAndSetPadding(pos, padding)
		} else {
			reader.Advance(util.TrimLeftSpaceLength(line[:len(line)-util.TrimRightSpaceLength(line)]))
		}
	}

	return parser.Continue | parser.HasChildren
}

// Close drops the admonition from the stack. goldmark calls it however the
// block ended -- a closing run, a dedent, or the end of the list or blockquote
// around it -- so the stack cannot keep an admonition that is no longer open.
//
// Only this admonition's own entry goes. A sibling that opened on the line
// that ended this one is already on the stack above it by now, because
// goldmark opens the new block before it closes the old.
func (b *admonitionParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {
	stack := admonitionStack(pc)
	for i, state := range stack {
		if state.node == node {
			pc.Set(admonitionStackKey, slices.Delete(slices.Clone(stack), i, i+1))

			return
		}
	}
}

func (b *admonitionParser) CanInterruptParagraph() bool {
	return true
}

func (b *admonitionParser) CanAcceptIndentedLine() bool {
	return false
}

// withoutLineEnding returns line without the newline it ends in, if it ends in
// one. The last line of a document need not.
func withoutLineEnding(line []byte) []byte {
	return bytes.TrimSuffix(line, []byte("\n"))
}

// hasAdmonitionClosingRun reports whether line closes state: a run of at least
// as many '!' as opened it, at the opening line's indentation and with nothing
// after it. newline is 1 when the line ends in one, for the caller to leave it
// unread.
func hasAdmonitionClosingRun(line []byte, w int, pos int, state *admonitionState) (closing bool, newline int) {
	if w != state.indent {
		return false, 0
	}

	i := pos
	for ; i < len(line) && line[i] == '!'; i++ {
	}
	if i-pos < state.length || !util.IsBlank(line[i:]) {
		return false, 0
	}

	if len(line) > 0 && line[len(line)-1] == '\n' {
		return true, 1
	}
	return true, 0
}

// htmlBlocks reads where an HTML block starts, for the look-ahead.
var htmlBlocks = parser.NewHTMLBlockParser()

// htmlBlockStart returns the kind of HTML block that line, without its
// indentation, starts where no paragraph is open, and 0 for a line that
// starts none: goldmark's own reading of the line, in a context with no
// block open.
func htmlBlockStart(line []byte, pc parser.Context) ast.HTMLBlockKind {
	if line[0] != '<' {
		return 0
	}

	node, _ := htmlBlocks.Open(nil, text.NewReader(line, text.IdentityDecoder), pc)
	if block, ok := node.(*ast.HTMLBlock); ok {
		return block.HTMLBlockKind
	}

	return 0
}

// closingRuns is what one parse has read ahead about the closing runs of its
// document.
type closingRuns struct {
	// last is where the last line that could close an admonition starts: a
	// run of '!' and nothing else, quote markers and indentation aside. It is
	// -1 without one. No admonition whose body starts after it has a closing
	// run.
	last int

	// ends tells, by where its first body line starts, where the closing run
	// of an admonition ends, or -1, so that a document of admonitions is not
	// read to its last run once for each. guessed are the ones among them
	// read from an opening line that goldmark may not read as one, and read
	// again from one it does.
	ends    map[int]int
	guessed map[int]bool

	// blocks is a context with no block open, to ask goldmark's parsers what
	// a line starts.
	blocks parser.Context

	// visits counts the lines read ahead, and depth how many admonitions
	// deep the reading went, deepest holding the most: what the tests bound
	// the work by.
	visits, depth, deepest int
}

// closingRunsKey holds the *closingRuns of the document being parsed.
var closingRunsKey = parser.NewContextKey()

func documentClosingRuns(pc parser.Context, source []byte) *closingRuns {
	if runs, ok := pc.Get(closingRunsKey).(*closingRuns); ok {
		return runs
	}

	runs := &closingRuns{last: -1, ends: map[int]int{}, guessed: map[int]bool{}, blocks: parser.NewContext()}

	for start := 0; start < len(source); {
		end := lineEnd(source, start)
		line := bytes.TrimLeft(source[start:end], " \t>")

		if n := runLength(line); n >= 3 && line[0] == '!' && util.IsBlank(line[n:]) {
			runs.last = start
		}

		start = end
	}

	pc.Set(closingRunsKey, runs)

	return runs
}

// lineEnd returns where the line that starts at start ends, its newline
// included.
func lineEnd(source []byte, start int) int {
	if i := bytes.IndexByte(source[start:], '\n'); i >= 0 {
		return start + i + 1
	}

	return len(source)
}

// runLength returns how many times line repeats its first byte before
// anything else.
func runLength(line []byte) int {
	n := 0
	for n < len(line) && line[n] == line[0] {
		n++
	}

	return n
}

// withoutContainerPrefix returns line without what the list items and
// blockquotes around an admonition take off it, given as prefix, which is
// what they took off its first body line. ok is false for a line outside
// them. The blank after a quote marker is optional, on either line; a blank
// line stays in a list item, and ends a quotation.
func withoutContainerPrefix(line []byte, prefix []byte) (rest []byte, ok bool) {
	for len(prefix) > 0 {
		if len(line) == 0 || line[0] != prefix[0] {
			return line, util.IsBlank(line) && bytes.IndexByte(prefix, '>') < 0
		}

		quote := prefix[0] == '>'
		line, prefix = line[1:], prefix[1:]

		if quote {
			line = bytes.TrimPrefix(line, []byte(" "))
			prefix = bytes.TrimPrefix(prefix, []byte(" "))
		}
	}

	return line, true
}

// closingRunAhead returns where the closing run of an admonition ends, or -1
// when it has none: one opened by a run of length '!' at indent, whose body
// starts level with its opening line on the line the reader is at.
func closingRunAhead(reader text.Reader, pc parser.Context, indent int, length int) int {
	source := reader.Source()
	_, segment := reader.PeekLine()
	start := bytes.LastIndexByte(source[:segment.Start], '\n') + 1

	// The columns of a tab the containers took only part of are indentation.
	prefix := untabbed(source[start:segment.Start])
	prefix = prefix[:max(0, len(prefix)-segment.Padding)]

	body := bodyLines{
		source: source,
		prefix: prefix,
		indent: indent,
		marker: '!',
		runs:   documentClosingRuns(pc, source),
	}

	return body.closingRun(start, length, true)
}

// bodyLines reads the lines of a body that is level with its opening line,
// as Continue will be given them: the ones in the same list item or
// blockquote, without what those take off each line.
type bodyLines struct {
	source []byte

	// prefix is what the list items and blockquotes took off the first body
	// line, a blank for each column of a tab: a later line may spell the same
	// columns with other blanks.
	prefix []byte

	// indent is the indentation of the opening line.
	indent int

	// marker is the character an opening or closing run is made of.
	marker byte

	runs *closingRuns
}

// paragraph is what the look-ahead knows about a paragraph being the
// innermost block open after a line. Only then does the next line go on it
// lazily, whatever indentation or quote marker that line lacks.
type paragraph int8

const (
	// paragraphUnknown is a line the look-ahead cannot tell about.
	paragraphUnknown paragraph = iota

	paragraphOpen
	paragraphClosed

	// paragraphHidden is a line of a body the look-ahead passes over, after
	// one that may have opened a code or HTML block there: it stays unknown
	// until that body ends.
	paragraphHidden
)

// after returns what is known once line is read, a line without its
// indentation where p was known before it, and whether the line is plain
// text: one that goes on an open paragraph, and opens one otherwise. Where it
// cannot tell it says unknown, which keeps the admonition from running on.
func (b *bodyLines) after(line []byte, p paragraph) (after paragraph, plain bool) {
	run := runLength(line)
	bare := util.IsBlank(line[run:])

	switch c := line[0]; {
	case c == '#':
		if run <= 6 && (bare || util.IsSpace(line[run])) {
			return paragraphClosed, false
		}
	case c == '>':
		// What is open in a blockquote is not followed.
		return paragraphUnknown, false
	case c == '`' || c == '~':
		if run >= 3 {
			return paragraphHidden, false
		}
	case c == '<':
		switch kind := htmlBlockStart(line, b.runs.blocks); {
		case kind == ast.HTMLBlockKind7 && p == paragraphOpen:
		case kind != 0:
			return paragraphHidden, false
		}
	case c == b.marker:
		if run >= 3 && bare {
			return paragraphUnknown, false
		} else if run >= 3 {
			return paragraphClosed, false
		}
	case c == '=' || c == '_' || c == ':' || c == '$' || c == '\\' || bytes.HasPrefix(line, []byte("[^")):
		// A setext underline, a thematic break, or what an extension may
		// take: a definition, a footnote, a formula.
		if c != '_' || isRule(line) {
			return paragraphUnknown, false
		}
	default:
		if _, text, ok := listItem(line); ok {
			if text != nil {
				after, _ = b.after(text, paragraphClosed)

				return after, false
			}

			return paragraphUnknown, false
		} else if isRule(line) || (c == '-' && bare) {
			return paragraphUnknown, false
		}
	}

	return paragraphOpen, true
}

// mayOpenLiteral reports whether line, or what the list items and blockquotes
// it opens hold of it, could open a code or HTML block: whether it starts
// with a tag or a code fence behind their markers, however far it is indented
// there.
func mayOpenLiteral(line []byte) bool {
	for {
		line = bytes.TrimLeft(line, " \t")

		marker := 0
		for marker < len(line) && marker < 9 && util.IsNumeric(line[marker]) {
			marker++
		}

		switch {
		case len(line) == 0:
			return false
		case line[0] == '>':
			line = line[1:]
		case marker > 0 && marker < len(line) && (line[marker] == '.' || line[marker] == ')'):
			line = line[marker+1:]
		case marker == 0 && len(line) > 1 && bytes.IndexByte([]byte("-+*"), line[0]) >= 0 && util.IsSpace(line[1]):
			line = line[1:]
		default:
			return line[0] == '<' || ((line[0] == '`' || line[0] == '~') && runLength(line) >= 3)
		}
	}
}

// isRule reports whether line is a thematic break: three or more of one
// character and nothing else but blanks.
func isRule(line []byte) bool {
	rule := bytes.Join(bytes.Fields(line), nil)

	return len(rule) >= 3 && strings.IndexByte("-*_", rule[0]) >= 0 && runLength(rule) == len(rule)
}

// lazy reports whether line, a line that the list item or blockquote around
// the body does not hold, or that is indented less than the opening line,
// goes on the paragraph open before it, which keeps everything around that
// paragraph open.
func (b *bodyLines) lazy(line []byte, p paragraph) bool {
	if p != paragraphOpen || util.IsBlank(line) {
		return false
	}

	// A run of the marker alone opens nothing, and where it cannot close
	// anything either it is text.
	line = bytes.TrimLeft(line, " \t")
	if run := runLength(line); line[0] == b.marker && util.IsBlank(line[run:]) {
		return true
	}

	_, plain := b.after(line, p)

	return plain
}

// closingRun returns where the closing run of an admonition ends, or -1 when
// it has none: the admonition opened by a run of length '!' whose body
// starts on the line at start.
func (b *bodyLines) closingRun(start int, length int, plain bool) int {
	if end, ok := b.runs.ends[start]; ok && (!plain || !b.runs.guessed[start]) {
		return end
	}

	b.runs.depth++
	b.runs.deepest = max(b.runs.deepest, b.runs.depth)

	end := b.findClosingRun(start, length, plain)
	b.runs.ends[start] = end
	b.runs.guessed[start] = !plain
	b.runs.depth--

	return end
}

// literal follows the code and HTML blocks of one body, whose text opens and
// closes nothing.
type literal struct {
	// fence and fenceLength are the fence of the code block the last line
	// was in, and html the kind of the HTML block it was in when that is one
	// that runs on over blank lines.
	fence       byte
	fenceLength int
	html        ast.HTMLBlockKind

	// markup is whether the last line was in an HTML block that ends at a
	// blank line, such as a <div>: an opening line there opens nothing,
	// while a run still closes. soft is whether the code or HTML block the
	// last line was in started in such a block, of which it is text, and so
	// ends with it.
	markup, soft bool
}

// blank reads a blank line.
func (l *literal) blank() {
	if l.markup = false; l.soft {
		l.soft, l.html, l.fenceLength = false, 0, 0
	}
}

// holds reports whether line is text of the code or HTML block open before
// it, the line that closes the block included. line starts with a run of run
// bytes, which bare says is all of it, and is indented beyond columns further
// than the body.
func (l *literal) holds(line []byte, run int, bare bool, beyond int) bool {
	switch {
	case l.html != 0:
		if htmlblock.Closes(l.html, line) {
			l.html = 0
		}
	case l.fenceLength > 0:
		if line[0] == l.fence && run >= l.fenceLength && bare && beyond <= 3 {
			l.fenceLength = 0
		}
	default:
		return false
	}

	return true
}

// opens reports whether line opens a code or HTML block. kind is the HTML
// block it starts where no paragraph is open, and noParagraph whether none is
// known to be: a block of the last kind cannot interrupt one.
func (l *literal) opens(line []byte, run int, kind ast.HTMLBlockKind, noParagraph bool) bool {
	switch {
	case (line[0] == '`' || line[0] == '~') && run >= 3 && (line[0] == '~' || bytes.IndexByte(line[run:], '`') < 0):
		l.fence, l.fenceLength, l.soft = line[0], run, l.markup
	case kind >= ast.HTMLBlockKind6:
		if l.markup || (kind == ast.HTMLBlockKind7 && !noParagraph) {
			return false
		}

		l.markup = true
	case kind != 0:
		if !htmlblock.Closes(kind, line) {
			l.html, l.soft = kind, l.markup
		}
	default:
		return false
	}

	return true
}

// findClosingRun reads on from start for the run that closes an admonition
// opened by length '!'. A line inside a fenced code block or an HTML block
// that runs on over blank lines is not one, and a run closes the nearest
// admonition above it that is still open: one opened in this body takes the
// run after its own unindented body, or straight after its indented one. A
// run straight after an indented one that was opened by a shorter run than
// this admonition closes this one instead when no later run does: it is
// longer than that one asks for.
//
// A line that goes on a paragraph lazily ends nothing: not the list item or
// blockquote whose indentation or marker it lacks, not this admonition when
// it is indented less, and not the indented body it is level with, which the
// next indented line then continues. plain is whether no line since the
// opening line that goldmark read could open a code or HTML block: where one
// may be open that the columns read here do not show, in a body this one
// holds, a line that looks like text may be its text, and none is taken to
// go on a paragraph.
func (b *bodyLines) findClosingRun(start int, length int, plain bool) int {
	var (
		// opened is the opening run of an admonition whose body has not
		// started, and indented that of one whose indented body the last
		// line was in, which bodyIndent is the indentation of. resume is
		// that of one whose indented body a lazy line left open. at is the
		// indentation of that admonition's opening line, which is this
		// one's or up to three columns more.
		opened     int
		indented   int
		bodyIndent int
		resume     int
		at         int

		// item is how far the content of the list item the last line was in
		// is indented, and 0 outside one.
		item int

		// here follows the code and HTML blocks of this body.
		here literal

		// text is whether a paragraph was open after the last line. The
		// body starts after an opening line, which leaves none. nested is
		// whether an admonition was opened here by a line indented further
		// than this one and has no indented body: the lines after it are
		// read by its indentation, which is not followed, until a line here
		// ends it.
		text   = paragraphClosed
		nested bool

		// spare is where the last run ends that closed an admonition opened
		// by a shorter run than this one and is long enough for this one, or
		// -1.
		spare = -1
	)

	for start < len(b.source) && start <= b.runs.last {
		lineStart := start
		start = lineEnd(b.source, lineStart)
		b.runs.visits++

		line, ok := withoutContainerPrefix(untabbed(b.source[lineStart:start]), b.prefix)
		if !ok {
			if plain && b.lazy(line, text) {
				continue
			}

			return spare
		}

		if util.IsBlank(line) {
			if here.blank(); text != paragraphHidden {
				text = paragraphClosed
			}

			continue
		}

		w, pos := util.IndentWidth(line, 0)
		if w < b.indent {
			if plain && b.lazy(line, text) {
				continue
			}

			return spare
		}

		line = line[pos:]
		run := runLength(line)
		bare := util.IsBlank(line[run:])

		if plain && mayOpenLiteral(line) {
			plain = false
		}

		if here.holds(line, run, bare, w-b.indent) {
			continue
		}

		deeper := w > b.indent

		switch {
		case opened > 0 && w > at:
			indented, opened, bodyIndent = opened, 0, w
		case opened > 0 && at > b.indent:
			opened, nested = 0, true
		case resume > 0 && w > at:
			indented, resume = resume, 0
		}

		// A line in an indented body, a list item or an indented code block
		// neither opens nor closes anything here. Any other line up to three
		// columns in can still open a code block or an HTML block.
		if beyond := b.beyond(w, indented > 0 && w > at, bodyIndent, item); beyond >= 0 {
			text = b.passed(line, run, beyond, text)

			continue
		}

		after, isText := b.after(line, text)

		// A line of text no deeper than the opening line of an indented body
		// goes on the paragraph that body ended in, and the body is still
		// open.
		lazy := plain && isText && text == paragraphOpen

		switch {
		case lazy && indented > 0:
			resume = indented
		case !lazy:
			resume = 0
		}

		// A run closes the admonition whose indented body it comes straight
		// after when it is level with that one's opening line, which only
		// one opened level with this one is here.
		above := 0
		if at == b.indent {
			above = indented
		}

		indented = 0

		if opened > 0 {
			nested := opened
			opened = 0

			// The admonition opened above has its body here. With a closing
			// run it holds the lines up to it; with none it ended at its
			// opening line, and neither does this one have a run when it
			// needs one as long. A list item its body opened with ended with
			// it.
			if end := b.closingRun(lineStart, nested, plain); end >= 0 {
				start, item, text = end, 0, paragraphClosed

				continue
			} else if length >= nested {
				return spare
			}
		}

		noParagraph := text == paragraphClosed
		text = after

		if !lazy {
			if item, _, _ = listItem(line); item > 0 {
				item += w - b.indent
			}
		}

		switch kind := htmlBlockStart(line, b.runs.blocks); {
		case here.opens(line, run, kind, noParagraph):
			text = paragraphClosed

			continue
		case here.markup:
			text = paragraphClosed
		case text == paragraphHidden:
			// Not a block after all: a tag in a paragraph that may be open,
			// or code quoted in one.
			text = paragraphUnknown
		}

		if nested = nested && (deeper || lazy); nested {
			text = paragraphUnknown
		}

		switch {
		case line[0] != b.marker || run < 3:
		case !bare:
			if !here.markup {
				opened, at = run, w
			}
		case deeper:
		case above > 0 && run >= above:
			// It closes the admonition with the indented body above it,
			// unless it turns out to be the last run this one could have.
			if length > above && run >= length {
				spare = start
			}
		case run >= length:
			return start
		}
	}

	return spare
}

// beyond returns how many columns a line indented w columns is indented
// beyond the content of what holds it, where the look-ahead passes over that:
// the indented body of an admonition opened here, when indented says the line
// is in one, whose first line is indented bodyIndent columns, a list item
// whose content is item columns in, or an indented code block. It returns -1
// for a line the look-ahead reads.
func (b *bodyLines) beyond(w int, indented bool, bodyIndent int, item int) int {
	switch {
	case indented:
		return max(0, w-bodyIndent)
	case item > 0 && w-b.indent >= item:
		return w - b.indent - item
	case w > b.indent+3:
		return w - b.indent
	}

	return -1
}

// passed reads a line of a body the look-ahead passes over, one starting
// with a run of run bytes and indented beyond columns further than that body,
// for whether a paragraph is open after it, where p says whether one was
// before it.
func (b *bodyLines) passed(line []byte, run int, beyond int, p paragraph) paragraph {
	// A list item, a blockquote or an admonition opened there holds lines of
	// its own, by its own indentation.
	_, _, item := listItem(line)
	if p == paragraphHidden || item || line[0] == '>' || (line[0] == b.marker && run >= 3 && !util.IsBlank(line[run:])) {
		return paragraphHidden
	}

	after, isText := b.after(line, p)

	switch {
	case isText && beyond > 3 && p != paragraphOpen:
		// Indented further than code is, the line is text of an open
		// paragraph only. Where none is open it is code, or a line of a body
		// further in.
		return paragraphHidden
	case after == paragraphHidden:
		return paragraphUnknown
	}

	return after
}

// untabbed returns line with each tab among its quote markers and indentation
// as the blanks it spans, tab stops being four columns apart as goldmark has
// them, so that there a byte is a column.
func untabbed(line []byte) []byte {
	lead := 0
	for lead < len(line) && (line[lead] == ' ' || line[lead] == '\t' || line[lead] == '>') {
		lead++
	}

	if bytes.IndexByte(line[:lead], '\t') < 0 {
		return line
	}

	spaced := make([]byte, 0, len(line)+3*lead)
	for _, c := range line[:lead] {
		if c == '\t' {
			spaced = append(spaced, "    "[:4-len(spaced)%4]...)
		} else {
			spaced = append(spaced, c)
		}
	}

	return append(spaced, line[lead:]...)
}

// listItem reports whether line starts a list item, and how many columns into
// the line the item's content is. text is what the item starts with, and nil
// for an item that starts with nothing or with indented code.
func listItem(line []byte) (content int, text []byte, ok bool) {
	marker := 0

	switch line[0] {
	case '-', '+', '*':
		// "* * *" is a thematic break.
		if isRule(line) {
			return 0, nil, false
		}

		marker = 1
	default:
		for marker < len(line) && marker < 9 && util.IsNumeric(line[marker]) {
			marker++
		}

		if marker == 0 || marker == len(line) || (line[marker] != '.' && line[marker] != ')') {
			return 0, nil, false
		}

		marker++
	}

	rest := line[marker:]
	if util.IsBlank(rest) {
		return marker + 1, nil, true
	}

	blanks, pos := util.IndentWidth(rest, marker)
	if blanks == 0 {
		return 0, nil, false
	}

	// More than four columns of blanks start the item with indented code.
	if blanks > 4 {
		return marker + 1, nil, true
	}

	return marker + blanks, rest[pos:], true
}

// attributesEndLine reports whether an attribute block in goldmark v1's
// grammar starts at the reader's position, blanks aside, and runs to the end
// of its line.
func attributesEndLine(source []byte, at text.Segment) bool {
	end, _, ok := syntax.AttributeBlock(source, at.Start)

	return ok && util.IsBlank(source[end:max(end, at.Stop)])
}
