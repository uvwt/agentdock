package tools

import (
	"strings"
	"testing"
)

func TestExtractMarkdownPathHintWithSpaces(t *testing.T) {
	obj := "將 PDF /Users/sigi/Documents/真理書密室/英文/RSSB/《靈性書信》Spiritual-Letters-Jaimal Singh.pdf 完整翻譯，輸出到 /Users/sigi/Documents/真理書密室/英文/RSSB/《靈性書信》Spiritual-Letters-Jaimal Singh_繁體中文.md。"
	md := extractMarkdownPathHint(obj)
	if !strings.HasSuffix(md, ".md") || !strings.Contains(md, "Spiritual-Letters-Jaimal Singh_繁體中文.md") {
		t.Fatalf("md path=%q", md)
	}
	pdf := extractPDFPathHint(obj)
	if !strings.HasSuffix(pdf, ".pdf") || !strings.Contains(pdf, "Spiritual-Letters-Jaimal Singh.pdf") {
		t.Fatalf("pdf path=%q", pdf)
	}
}
