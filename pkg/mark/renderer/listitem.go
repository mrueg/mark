package renderer

import (
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type ConfluenceListItemRenderer struct {
	html.Config
}

// NewConfluenceRenderer creates a new instance of the ConfluenceRenderer
func NewConfluenceListItemRenderer(opts ...html.Option) renderer.NodeRenderer {
	return &ConfluenceListItemRenderer{
		Config: html.NewConfig(),
	}
}

// RegisterFuncs implements NodeRenderer.RegisterFuncs .
func (r *ConfluenceListItemRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindListItem, r.renderListItem)
}

func (r *ConfluenceListItemRenderer) renderListItem(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		if n.FirstChild() != nil && n.FirstChild().FirstChild() != nil && n.FirstChild().FirstChild().Kind() == east.KindTaskCheckBox {
			_, _ = w.WriteString("<ac:task>")
		} else {
			if n.Attributes() != nil {
				_, _ = w.WriteString("<li")
				html.RenderAttributes(w, n, html.ListItemAttributeFilter)
				_ = w.WriteByte('>')
			} else {
				_, _ = w.WriteString("<li>")
			}
		}
		if n.FirstChild() != nil {
			if _, ok := n.FirstChild().(*ast.TextBlock); !ok {
				_ = w.WriteByte('\n')
			}
		}
	} else {
		if n.FirstChild() != nil && n.FirstChild().FirstChild() != nil && n.FirstChild().FirstChild().Kind() == east.KindTaskCheckBox {
			_, _ = w.WriteString("</span></ac:task-body>\n</ac:task>\n")
		} else {
			_, _ = w.WriteString("</li>\n")
		}
	}
	return ast.WalkContinue, nil
}
