package renderer

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"net/url"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/kovetskiy/mark/v16/attachment"
	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// MathInlineKind is the AST Kind for inline math formulas ($...$ or \(...\)).
var MathInlineKind = ast.NewNodeKind("MathInline")

// MathBlockKind is the AST Kind for display/block math formulas ($$...$$ or \[...\]).
var MathBlockKind = ast.NewNodeKind("MathBlock")

// MathFormulaProvider is an interface for nodes that contain a math formula string.
type MathFormulaProvider interface {
	GetFormula() string
}

// ProcessMathPNG renders a LaTeX math formula to a PNG image attachment.
func ProcessMathPNG(formula string, displayMode bool) (attachment.Attachment, error) {
	checkSum, err := attachment.GetChecksum(bytes.NewReader([]byte(formula)))
	if err != nil {
		return attachment.Attachment{}, err
	}

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.css">
  <script src="https://cdn.jsdelivr.net/npm/katex@0.16.11/dist/katex.min.js"></script>
  <style>
    body { margin: 0; padding: 4px; background: transparent; display: inline-block; }
    #container { display: inline-block; padding: 4px; }
  </style>
</head>
<body>
  <div id="container"></div>
  <script>
    try {
      katex.render(%q, document.getElementById('container'), {
        displayMode: %t,
        throwOnError: false
      });
    } catch (e) {
      document.getElementById('container').innerText = e.message;
    }
  </script>
</body>
</html>`, formula, displayMode)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)...)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	var pngBytes []byte
	err = chromedp.Run(taskCtx,
		chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(htmlContent)),
		chromedp.WaitVisible("#container", chromedp.ByID),
		chromedp.Screenshot("#container", &pngBytes, chromedp.ByID),
	)

	fileName := "math-" + checkSum + ".png"

	if err != nil {
		svgContent := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="300" height="40"><text x="10" y="25" font-family="monospace" font-size="14" fill="#333">%s</text></svg>`, html.EscapeString(formula))
		return attachment.Attachment{
			Name:      checkSum,
			Filename:  "math-" + checkSum + ".svg",
			FileBytes: []byte(svgContent),
			Checksum:  checkSum,
			Replace:   formula,
		}, nil
	}

	return attachment.Attachment{
		Name:      checkSum,
		Filename:  fileName,
		FileBytes: pngBytes,
		Checksum:  checkSum,
		Replace:   formula,
	}, nil
}

// ConfluenceMathRenderer renders MathInline and MathBlock nodes into PNG image attachments embedded as ac:image tags.
type ConfluenceMathRenderer struct {
	Stdlib      *stdlib.Lib
	Attachments attachment.Attacher
}

func NewConfluenceMathRenderer(stdlib *stdlib.Lib, attachments attachment.Attacher) renderer.NodeRenderer {
	return &ConfluenceMathRenderer{
		Stdlib:      stdlib,
		Attachments: attachments,
	}
}

func (r *ConfluenceMathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(MathInlineKind, r.renderMathInline)
	reg.Register(MathBlockKind, r.renderMathBlock)
}

func (r *ConfluenceMathRenderer) renderMathInline(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	provider, ok := node.(MathFormulaProvider)
	if !ok {
		return ast.WalkContinue, nil
	}
	formula := provider.GetFormula()

	att, err := ProcessMathPNG(formula, false)
	if err != nil {
		return ast.WalkStop, fmt.Errorf("math rendering failed for %q: %w", formula, err)
	}
	if r.Attachments != nil {
		r.Attachments.Attach(att)
	}

	if r.Stdlib != nil && r.Stdlib.Templates != nil {
		err = r.Stdlib.Templates.ExecuteTemplate(
			w,
			"ac:image",
			struct {
				Align          string
				Layout         string
				OriginalWidth  string
				OriginalHeight string
				Width          string
				Height         string
				Title          string
				Alt            string
				Attachment     string
				Url            string
			}{
				Attachment: att.Filename,
				Title:      formula,
			},
		)
		return ast.WalkContinue, err
	}

	_, err = fmt.Fprintf(w, `<ac:image><ri:attachment ri:filename="%s"/></ac:image>`, att.Filename)
	return ast.WalkContinue, err
}

func (r *ConfluenceMathRenderer) renderMathBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	provider, ok := node.(MathFormulaProvider)
	if !ok {
		return ast.WalkContinue, nil
	}
	formula := provider.GetFormula()

	att, err := ProcessMathPNG(formula, true)
	if err != nil {
		return ast.WalkStop, fmt.Errorf("math rendering failed for %q: %w", formula, err)
	}
	if r.Attachments != nil {
		r.Attachments.Attach(att)
	}

	if r.Stdlib != nil && r.Stdlib.Templates != nil {
		err = r.Stdlib.Templates.ExecuteTemplate(
			w,
			"ac:image",
			struct {
				Align          string
				Layout         string
				OriginalWidth  string
				OriginalHeight string
				Width          string
				Height         string
				Title          string
				Alt            string
				Attachment     string
				Url            string
			}{
				Align:      "center",
				Attachment: att.Filename,
				Title:      formula,
			},
		)
		return ast.WalkSkipChildren, err
	}

	_, err = fmt.Fprintf(w, `<ac:image ac:align="center"><ri:attachment ri:filename="%s"/></ac:image>`, att.Filename)
	return ast.WalkSkipChildren, err
}
