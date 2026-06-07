package textutil

import "unicode/utf8"

type Truncation struct {
	Text      string
	Truncated bool
	Omitted   int
}

func SafeTruncateString(value string, maxBytes int) Truncation {
	if maxBytes <= 0 || len([]byte(value)) <= maxBytes {
		return Truncation{Text: value}
	}
	cut := safeUTF8Boundary([]byte(value), maxBytes)
	return Truncation{Text: value[:cut], Truncated: true, Omitted: len([]byte(value)) - cut}
}

func SafeTruncateBytes(data []byte, maxBytes int) Truncation {
	if maxBytes <= 0 || len(data) <= maxBytes {
		return Truncation{Text: string(data)}
	}
	cut := safeUTF8Boundary(data, maxBytes)
	return Truncation{Text: string(data[:cut]), Truncated: true, Omitted: len(data) - cut}
}

func safeUTF8Boundary(data []byte, maxBytes int) int {
	if maxBytes <= 0 || maxBytes >= len(data) {
		return len(data)
	}
	cut := maxBytes
	for cut > 0 && !utf8.Valid(data[:cut]) {
		cut--
	}
	return cut
}
