package mark

import (
	"testing"

	"github.com/kovetskiy/mark/v16/stdlib"
	"github.com/kovetskiy/mark/v16/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMathFormulaRendering(t *testing.T) {
	markdownInput := []byte(`Inline math: $e = mc^2$

Display math:
$$
\int_0^\infty e^{-x^2} dx = \frac{\sqrt{\pi}}{2}
$$
`)

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	cfg := types.MarkConfig{
		Features: []string{"math"},
	}

	htmlOutput, attachments, err := CompileMarkdown(markdownInput, std, "test.md", cfg)
	require.NoError(t, err)

	assert.Len(t, attachments, 2)
	assert.Contains(t, htmlOutput, `<ac:image`)
	assert.Contains(t, htmlOutput, `<ri:attachment ri:filename="math-`)
}

func TestMathFencedCodeBlockRendering(t *testing.T) {
	markdownInput := []byte("```math\na^2 + b^2 = c^2\n```\n")

	std, err := stdlib.New(nil)
	require.NoError(t, err)

	cfg := types.MarkConfig{
		Features: []string{"math"},
	}

	htmlOutput, attachments, err := CompileMarkdown(markdownInput, std, "test.md", cfg)
	require.NoError(t, err)

	assert.Len(t, attachments, 1)
	assert.Contains(t, htmlOutput, `<ac:image`)
	assert.Contains(t, htmlOutput, `<ri:attachment ri:filename="math-`)
}
