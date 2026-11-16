package parser_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kovetskiy/mark/v17/internal/admonitiongen"
	cparser "github.com/kovetskiy/mark/v17/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/util"
)

// The admonition parser decides at an admonition's first body line whether a
// closing run lies ahead, by reading the raw lines itself. That reading is a
// second account of where code and HTML blocks, list items and blockquotes
// start and end, and it is wrong wherever it and goldmark disagree. The tests
// here write documents out of the pieces the two have disagreed about and hold
// one to the other.

// differentialSeed and differentialDocuments fix the documents the seeded
// test reads, so that a failure names a document anyone can read again: the
// one admonitiongen.New writes from rand.New(rand.NewSource(seed)).
const (
	differentialSeed      = 20240611
	differentialDocuments = 2000
)

var admonitionBlocks = parser.New(
	parser.WithBlockParsers(util.Prioritized(cparser.NewAdmonitionParser(), 100)),
)

// reading is a document as the admonition parser read it.
type reading struct {
	source []byte
	doc    ast.Node
	work   cparser.LookAheadWork
}

func readAdmonitions(source string) reading {
	pc := parser.NewContext()
	src := []byte(source)

	return reading{source: src, doc: admonitionBlocks.Parse(src, parser.WithContext(pc)), work: cparser.LookAheadWorkOf(pc)}
}

var generatedWord = regexp.MustCompile(`\b[wt][0-9]+\b`)

// text returns what a block holds itself, its children's text aside.
func (r reading) text(node ast.Node) string {
	switch n := node.(type) {
	case *cparser.Admonition:
		return string(n.Title)
	case *ast.CodeBlock:
		return n.Value.Str(r.source)
	case *ast.HTMLBlock:
		return n.Value.Str(r.source)
	case ast.BlockNode:
		var text strings.Builder
		for _, segment := range n.Source() {
			text.Write(segment.Bytes(r.source))
		}

		return text.String()
	}

	return ""
}

// tree returns the blocks in document order, one a line and indented by
// depth: each as its kind and the generated words it holds, so that two
// spellings of the same blanks read alike.
func (r reading) tree() []string {
	var lines []string

	var walk func(node ast.Node, depth string)
	walk = func(node ast.Node, depth string) {
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if _, ok := child.(ast.BlockNode); !ok {
				continue
			}

			line := depth + child.Kind().String()
			if admonition, ok := child.(*cparser.Admonition); ok {
				line += "!" + string(admonition.AdmonitionClass)
			}

			lines = append(lines, line+" "+strings.Join(generatedWord.FindAllString(r.text(child), -1), " "))
			walk(child, depth+"  ")
		}
	}
	walk(r.doc, "")

	return lines
}

// words returns the generated words in the order the tree holds them.
func (r reading) words() []string {
	var words []string

	_ = ast.Walk(r.doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if _, ok := node.(ast.BlockNode); entering && ok && node.Kind() != ast.KindDocument {
			words = append(words, generatedWord.FindAllString(r.text(node), -1)...)
		}

		return ast.WalkContinue, nil
	})

	return words
}

// lookAheadAgrees holds what was read ahead to what was parsed: an admonition
// with no closing run ahead has no body but what its opening line started,
// and one kept open for a closing run is not open past it. The run closes it, or a run no later closes an
// admonition around it and it with that one. A line indented less than the
// body it is in reads level with two admonitions at once, which are then both
// kept open for one run; and one admonition's run is sometimes read ahead
// twice, the first time from an opening line that may be text of a code or
// HTML block, when no line is taken to go on a paragraph lazily and the run
// found for the admonition around it may come sooner.
func (r reading) lookAheadAgrees() error {
	return ast.Walk(r.doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		admonition, ok := node.(*cparser.Admonition)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}

		ahead := cparser.LookAheadOf(admonition)
		line := 1 + strings.Count(string(r.source[:admonition.Pos()]), "\n")

		switch {
		case !ahead.Asked:
		case ahead.RunEnd < 0 && (ahead.ClosedAt != 0 || r.holdsLaterLines(admonition)):
			return ast.WalkStop, fmt.Errorf("line %d: no closing run was found ahead, and the admonition has a body", line)
		case ahead.RunEnd >= 0 && !closedBy(admonition, ahead.RunEnd):
			return ast.WalkStop, fmt.Errorf(
				"line %d: kept open for the closing run that ends at byte %d, and closed at byte %d (0: by no run)",
				line, ahead.RunEnd, ahead.ClosedAt)
		}

		return ast.WalkContinue, nil
	})
}

// holdsLaterLines reports whether a block of the admonition's starts after
// its opening line. A paragraph that starts on that line, with what is left
// of it when a "{" there is no attribute block, goes on in the lines after it
// as any paragraph does.
func (r reading) holdsLaterLines(admonition *cparser.Admonition) bool {
	for child := admonition.FirstChild(); child != nil; child = child.NextSibling() {
		if r.lineOf(child.Pos()) != r.lineOf(admonition.Pos()) {
			return true
		}
	}

	return false
}

// closedBy reports whether the run that ends at end closed node, or a run
// that ends no later closed an admonition around it.
func closedBy(node ast.Node, end int) bool {
	if cparser.LookAheadOf(node.(*cparser.Admonition)).ClosedAt == end {
		return true
	}

	for node = node.Parent(); node != nil; node = node.Parent() {
		if admonition, ok := node.(*cparser.Admonition); ok {
			if at := cparser.LookAheadOf(admonition).ClosedAt; at > 0 && at <= end {
				return true
			}
		}
	}

	return false
}

// lineOf returns the number of the line that holds byte at, counting from 0.
func (r reading) lineOf(at int) int {
	return strings.Count(string(r.source[:at]), "\n")
}

// column returns the column of byte at in its line, a tab reaching to the
// next of the tab stops four columns apart.
func (r reading) column(at int) int {
	column := 0
	for _, c := range r.source[strings.LastIndexByte(string(r.source[:at]), '\n')+1 : at] {
		if c == '\t' {
			column = column/4*4 + 4
		} else {
			column++
		}
	}

	return column
}

// closedInLiteralText returns an error for an admonition a line inside a
// fenced code block or an HTML block closed, cutting the block in two: one
// opened before a block the document was written with, where the tree has
// that block, and closed by a line between the block's opening and closing
// lines.
func (r reading) closedInLiteralText(literal [][2]int) error {
	blocks := map[int]bool{}

	// closed is the lines admonitions were opened on and closed by.
	var closed [][2]int

	_ = ast.Walk(r.doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.CodeBlock:
			if n.CodeBlockKind == ast.CodeBlockKindFenced {
				blocks[r.lineOf(n.Pos())] = true
			}
		case *ast.HTMLBlock:
			blocks[r.lineOf(n.Pos())] = true
		case *cparser.Admonition:
			if at := cparser.LookAheadOf(n).ClosedAt; at > 0 {
				closed = append(closed, [2]int{r.lineOf(n.Pos()), r.lineOf(at - 1)})
			}
		}

		return ast.WalkContinue, nil
	})

	for _, admonition := range closed {
		for _, block := range literal {
			if admonition[0] < block[0] && block[0] < admonition[1] && admonition[1] < block[1] && blocks[block[0]] {
				return fmt.Errorf("line %d closed the admonition opened on line %d, and is text of the block that opens on line %d",
					admonition[1]+1, admonition[0]+1, block[0]+1)
			}
		}
	}

	return nil
}

// runLeftAsText returns an error for an admonition that ended at its opening
// line while a closing run for it lay ahead: a paragraph after it, in the
// same list item, blockquote or body, with a line of nothing but a run as
// long as its own in its column.
//
// It looks only at an admonition that is not indented in what holds it. The
// lines after an indented one are read some columns further in while it is
// open, so the tree of it closed does not say what it would have held.
func (r reading) runLeftAsText(marker byte) error {
	run := func(at int) int {
		n := 0
		for at+n < len(r.source) && r.source[at+n] == marker {
			n++
		}

		return n
	}

	return ast.Walk(r.doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		admonition, ok := node.(*cparser.Admonition)
		if !entering || !ok {
			return ast.WalkContinue, nil
		}

		if ahead := cparser.LookAheadOf(admonition); !ahead.Asked || ahead.RunEnd >= 0 || ahead.Indent != 0 {
			return ast.WalkContinue, nil
		}

		opening := admonition.Pos()
		column, length := r.column(opening), run(opening)

		for sibling := node.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
			paragraph, ok := sibling.(*ast.Paragraph)
			if !ok {
				continue
			}

			for _, segment := range paragraph.Source() {
				n := run(segment.Start)
				if n < length || strings.TrimSpace(string(r.source[segment.Start+n:segment.Stop])) != "" {
					continue
				}

				if r.column(segment.Start) == column {
					return ast.WalkStop, fmt.Errorf(
						"line %d: the admonition ended at its opening line, and the run on line %d would have closed it",
						r.lineOf(opening)+1, r.lineOf(segment.Start)+1)
				}
			}
		}

		return ast.WalkContinue, nil
	})
}

// regular is the documents in which a closing run left as text is the
// look-ahead's mistake. The look-ahead reads the lines of a body by their
// columns, so the others are those where a column says less: an admonition
// indented in the body of another, or a line indented between an opening line
// and its body, which shift the lines after them; and a code or HTML block
// left unclosed, which ends with the admonition it is in and not otherwise.
func regular() admonitiongen.Options {
	o := admonitiongen.All()
	o.Indents, o.Ragged, o.Unclosed, o.LazyDeep = false, false, false, false

	return o
}

// checkAdmonitionDocument reads a generated document and returns what is
// wrong with the reading: the look-ahead and the parse disagreeing, a code or
// HTML block cut in two, text lost, repeated or out of order, or, in a
// regular document, a closing run left as text.
func checkAdmonitionDocument(document admonitiongen.Document, regular bool) error {
	r := readAdmonitions(document.Text)

	if err := r.lookAheadAgrees(); err != nil {
		return err
	}

	if err := r.closedInLiteralText(document.Literal); err != nil {
		return err
	}

	if regular {
		if err := r.runLeftAsText('!'); err != nil {
			return err
		}
	}

	if want, got := generatedWord.FindAllString(document.Text, -1), r.words(); !slices.Equal(want, got) {
		return fmt.Errorf("the document's words are %v and the tree's are %v", want, got)
	}

	return nil
}

// checkTabbedAlike reads a document with some of its blanks written as tabs
// and requires the tree the spaces give.
func checkTabbedAlike(r admonitiongen.Rand, document admonitiongen.Document, regular bool) error {
	tabbed := document.Tabbed(r)
	if tabbed.Text == document.Text {
		return nil
	}

	if err := checkAdmonitionDocument(tabbed, regular); err != nil {
		return fmt.Errorf("with tabs, %q: %w", tabbed.Text, err)
	}

	if want, got := readAdmonitions(document.Text).tree(), readAdmonitions(tabbed.Text).tree(); !slices.Equal(want, got) {
		return fmt.Errorf("with tabs, %q, the tree is\n%s\nand with spaces\n%s",
			tabbed.Text, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	return nil
}

// checkGenerated writes a document from r's choices, any document or a
// regular one, and checks it as written and with tabs.
func checkGenerated(r admonitiongen.Rand, regularOnly bool) error {
	o := admonitiongen.All()
	if regularOnly {
		o = regular()
	}

	document := admonitiongen.New(r, o)

	if err := checkAdmonitionDocument(document, regularOnly); err != nil {
		return fmt.Errorf("document %q: %w", document.Text, err)
	}

	if err := checkTabbedAlike(r, document, regularOnly); err != nil {
		return fmt.Errorf("document %q: %w", document.Text, err)
	}

	return nil
}

// TestAdmonitionLookAheadAgreesWithTheParse reads generated documents and
// requires of each that every admonition kept open for a closing run ends at
// it, that none ends inside a code or HTML block, that no text is lost,
// repeated or moved, and that tabs read as the blanks they span; and of the
// regular ones, that none ends before a run that would have closed it.
func TestAdmonitionLookAheadAgreesWithTheParse(t *testing.T) {
	for n := range differentialDocuments {
		seed := int64(differentialSeed + n)

		if err := checkGenerated(rand.New(rand.NewSource(seed)), n%2 == 1); err != nil {
			t.Fatalf("seed %d, regular %v, %v", seed, n%2 == 1, err)
		}
	}
}

// FuzzAdmonitionDocuments is TestAdmonitionLookAheadAgreesWithTheParse for
// documents written from the fuzzer's bytes, which are also read as a
// document themselves: the look-ahead has to agree with the parse on any
// input.
func FuzzAdmonitionDocuments(f *testing.F) {
	for _, seed := range []string{
		"",
		"!!! note \"T\"\nbody\n ```\n!!!\n ```\nmore\n!!!\nafter\n",
		">\t!!! note\n>\tbody\n>   !!!\n\nafter\n",
		"!!!! quote \"T\"\nw2\n\n!!! note \"N\"\n    w5\n\n!!!!\nafter\n",
		"- !!! note\n  text\ntext\n  !!!\n",
		"\x04\x09\x03\x01\x02\x05\x08\x00\x07\x06\x0b\x0a\x0d\x0c\x04\x04\x09\x09\x03\x03\x08\x08",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if err := readAdmonitions(string(data)).lookAheadAgrees(); err != nil {
			t.Fatalf("document %q: %v", data, err)
		}

		for _, regularOnly := range []bool{false, true} {
			choices := admonitiongen.Bytes(data)
			if err := checkGenerated(&choices, regularOnly); err != nil {
				t.Fatalf("regular %v, %v", regularOnly, err)
			}
		}
	})
}

// TestAdmonitionsReadAsBefore holds the forms this parser has not changed
// the reading of to the reading they had: testdata/admonition-master.json
// holds generated documents with the tree the parser gave them before a body
// level with its opening line had to end at a closing run. They are of three
// classes. In "indented" every admonition has an indented body and no line
// of a closing run is written; in "closed" a body may be level with its
// opening line, and then ends at a closing run of its own; "lazy" is "closed"
// with paragraphs that go on in lines lacking the indentation or the markers
// of what they are in. Some of each are written with tabs.
//
// The trees are recorded rather than derived, because the reading they hold
// is what documents were written against. The file is made once: the
// documents by admonitiongen, each tree by parsing its document with the
// parser of the commit the change was made on. Of the lazy ones, those are
// left out where a closing run follows lines level with an indented body
// inside the admonition: that run now closes the admonition, and used to
// close the body inside it and leave the admonition open to the end.
func TestAdmonitionsReadAsBefore(t *testing.T) {
	data, err := os.ReadFile("testdata/admonition-master.json")
	require.NoError(t, err)

	var documents []struct {
		Class  string   `json:"class"`
		Source string   `json:"source"`
		Blocks []string `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal(data, &documents))
	require.NotEmpty(t, documents)

	for n, document := range documents {
		r := readAdmonitions(document.Source)

		require.NoError(t, r.lookAheadAgrees(), "document %d, %s: %q", n, document.Class, document.Source)
		require.Equal(t, document.Blocks, r.tree(), "document %d, %s: %q", n, document.Class, document.Source)
	}
}

// TestAdmonitionLookAheadReadsEachLineOnce: a page of admonitions that end
// at their opening lines is read ahead once, not to its last closing run for
// each of them, whatever keeps each from that run. The lines read are
// counted, since a clock says as much about the machine as about the parser.
func TestAdmonitionLookAheadReadsEachLineOnce(t *testing.T) {
	const count = 2000

	for name, document := range map[string]string{
		"a run at the end":        strings.Repeat("!!! note \"T\"\n\ntext\n\n", count) + "!!!\n",
		"a run in code":           strings.Repeat("!!! note \"T\"\n\ntext\n\n", count) + "```\n!!!\n```\n",
		"one straight after one":  strings.Repeat("!!! a\ntext\n", count) + "!!!\n",
		"longer and shorter runs": strings.Repeat("!!! a\ntext\n!!!! a\ntext\n", count) + "!!!\n",
		"each closed":             strings.Repeat("!!! a\ntext\n!!!\n", count),
		"each in a list item":     strings.Repeat("- !!! a\n  text\n", count) + "- !!!\n",
	} {
		t.Run(name, func(t *testing.T) {
			r := readAdmonitions(document)

			require.NoError(t, r.lookAheadAgrees())
			assert.LessOrEqual(t, r.work.Visits, 2*strings.Count(document, "\n"))
		})
	}
}
