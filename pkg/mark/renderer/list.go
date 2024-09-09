package renderer

import (
	"fmt"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceListRenderer struct {
	html.Config
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceListRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceListRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceListRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindList, r.renderList)
}

func (r *ConfluenceListRenderer) renderList(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.List)
	tag := "ul"
	if n.IsOrdered() {
		tag = "ol"
	}
	if entering {
		if n.FirstChild().FirstChild().FirstChild().Kind() == east.KindTaskCheckBox {
			_, _ = w.WriteString("<ac:task-list>\n")
		} else {
			_ = w.WriteByte('<')
			_, _ = w.WriteString(tag)
			if n.IsOrdered() && n.Start != 1 {
				_, _ = fmt.Fprintf(w, " start=\"%d\"", n.Start)
			}
			if n.Attributes() != nil {
				html.RenderAttributes(w, n, html.ListAttributeFilter)
			}
			_, _ = w.WriteString(">\n")
		}
	} else {
		if n.FirstChild().FirstChild().FirstChild().Kind() == east.KindTaskCheckBox {
			_, _ = w.WriteString("</ac:task-list>\n")

		} else {
			_, _ = w.WriteString("</")
			_, _ = w.WriteString(tag)
			_, _ = w.WriteString(">\n")
		}
	}
	return ast.WalkContinue, nil
}
