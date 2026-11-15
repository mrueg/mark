// Package parsenote carries what a block parser found about a document that
// its author should hear, from the parse to the compile that reports it.
//
// A goldmark block parser returns a node and nothing else, so a finding
// travels in the context of the parse, under a key kept here. The package
// imports none of mark's, so a parser and what assembles the parsers can both
// use it.
package parsenote

import "github.com/yuin/goldmark/v2/parser"

var strandedBodiesKey = parser.NewContextKey()

// StrandedBody records an admonition as one whose body is published after it
// rather than in it: the lines that follow its opening line are not indented
// under it, and no closing run ends them. at is where the opening run starts
// in the source.
func StrandedBody(pc parser.Context, at int) {
	pc.Set(strandedBodiesKey, append(StrandedBodies(pc), at))
}

// StrandedBodies returns where the admonitions recorded by StrandedBody
// start, in the order they were recorded.
func StrandedBodies(pc parser.Context) []int {
	at, _ := pc.Get(strandedBodiesKey).([]int)

	return at
}
