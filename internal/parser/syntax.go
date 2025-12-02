package parser

import (
	"bytes"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/alecthomas/chroma/v2/styles"
)

func init() {
	// Initialize chroma styles
	_ = styles.Get("dracula")
}

// Highlight applies syntax highlighting to content using chroma
// format can be "json", "yaml", etc.
func Highlight(content, format string) string {
	var buf bytes.Buffer
	err := quick.Highlight(&buf, content, format, "terminal256", "dracula")
	if err != nil {
		return content
	}
	return buf.String()
}
