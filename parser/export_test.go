package parser

import "github.com/yuin/goldmark/v2/parser"

// LookAhead is what the parser read ahead about one admonition's closing run,
// and what came of it.
type LookAhead struct {
	// Asked is whether a closing run was looked for.
	Asked bool

	// Indent is the indentation of the opening line, in what the list item,
	// blockquote or body around it holds.
	Indent int

	// RunEnd is where the run that was found ends, and -1 when none was.
	RunEnd int

	// ClosedAt is where the run that closed the admonition ends, and 0 for
	// one that no run closed.
	ClosedAt int
}

// LookAheadOf returns what was read ahead for n.
func LookAheadOf(n *Admonition) LookAhead {
	return LookAhead{Asked: n.lookAhead.asked, Indent: n.lookAhead.indent, RunEnd: n.lookAhead.runEnd, ClosedAt: n.lookAhead.closedAt}
}

// LookAheadWork is how much one parse read ahead.
type LookAheadWork struct {
	// Visits is the number of lines read ahead, a line counting each time it
	// is read.
	Visits int

	// Depth is how many admonitions deep the reading went at most.
	Depth int
}

// LookAheadWorkOf returns how much the parse that pc belongs to read ahead.
func LookAheadWorkOf(pc parser.Context) LookAheadWork {
	runs, ok := pc.Get(closingRunsKey).(*closingRuns)
	if !ok {
		return LookAheadWork{}
	}

	return LookAheadWork{Visits: runs.visits, Depth: runs.deepest}
}
