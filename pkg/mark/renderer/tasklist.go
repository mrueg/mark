package renderer

import (
	gast "github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceTaskCheckBoxHTMLRenderer struct {
	html.Config
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceTaskCheckBoxHTMLRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceTaskCheckBoxHTMLRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceTaskCheckBoxHTMLRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(east.KindTaskCheckBox, r.renderTaskCheckBox)
}

func (r *ConfluenceTaskCheckBoxHTMLRenderer) renderTaskCheckBox(
	w util.BufWriter, source []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if !entering {
		return gast.WalkContinue, nil
	}
	n := node.(*east.TaskCheckBox)

	if n.IsChecked {
		_, _ = w.WriteString(`<ac:task-status>complete</ac:task-status>`)
	} else {
		_, _ = w.WriteString(`<ac:task-status>incomplete</ac:task-status>`)
	}
	_, _ = w.WriteString("\n<ac:task-body><span>")
	return gast.WalkContinue, nil
}
