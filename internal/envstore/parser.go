package envstore

import (
	"fmt"
	"sort"
	"strings"
)

func parse(data []byte) (map[string]string, error) {
	input := string(data)
	values := map[string]string{}
	for index := 0; ; {
		index = skipSpaceAndComments(input, index)
		if index >= len(input) {
			return values, nil
		}

		if strings.HasPrefix(input[index:], "export") {
			after := index + len("export")
			if after < len(input) && (input[after] == ' ' || input[after] == '\t') {
				index = skipHorizontalSpace(input, after)
			}
		}

		start := index
		for index < len(input) && isEnvKeyByte(input[index], index == start) {
			index++
		}
		if start == index {
			return nil, fmt.Errorf("line %d: expected environment variable name", lineNumber(input, index))
		}
		key := input[start:index]
		if err := ValidateKey(key); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber(input, start), err)
		}
		index = skipHorizontalSpace(input, index)
		if index >= len(input) || input[index] != '=' {
			return nil, fmt.Errorf("line %d: expected '=' after %s", lineNumber(input, index), key)
		}
		index++
		index = skipHorizontalSpace(input, index)

		value, next, err := parseValue(input, index)
		if err != nil {
			return nil, fmt.Errorf("line %d: parse %s: %w", lineNumber(input, index), key, err)
		}
		values[key] = value
		index = next
	}
}

func parseValue(input string, index int) (string, int, error) {
	var value strings.Builder
	var pendingSpace strings.Builder
	quote := byte(0)

	flushPending := func() {
		if pendingSpace.Len() > 0 {
			value.WriteString(pendingSpace.String())
			pendingSpace.Reset()
		}
	}

	for index < len(input) {
		char := input[index]
		switch quote {
		case '\'':
			if char == '\'' {
				quote = 0
				index++
				continue
			}
			value.WriteByte(char)
			index++
			continue
		case '"':
			if char == '"' {
				quote = 0
				index++
				continue
			}
			if char == '\\' && index+1 < len(input) {
				next := input[index+1]
				switch next {
				case '"', '\\', '$', '`':
					value.WriteByte(next)
					index += 2
					continue
				case '\n':
					index += 2
					continue
				}
			}
			value.WriteByte(char)
			index++
			continue
		}

		switch char {
		case '\'', '"':
			flushPending()
			quote = char
			index++
		case '\\':
			flushPending()
			if index+1 >= len(input) {
				return "", index, fmt.Errorf("trailing escape")
			}
			if input[index+1] == '\n' {
				index += 2
				continue
			}
			value.WriteByte(input[index+1])
			index += 2
		case ' ', '\t', '\r':
			pendingSpace.WriteByte(char)
			index++
		case '#':
			if value.Len() == 0 || pendingSpace.Len() > 0 {
				for index < len(input) && input[index] != '\n' {
					index++
				}
				if index < len(input) {
					index++
				}
				return value.String(), index, nil
			}
			flushPending()
			value.WriteByte(char)
			index++
		case '\n':
			return value.String(), index + 1, nil
		default:
			flushPending()
			value.WriteByte(char)
			index++
		}
	}
	if quote != 0 {
		return "", index, fmt.Errorf("unterminated quoted value")
	}
	return value.String(), index, nil
}

func marshal(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var output strings.Builder
	for _, key := range keys {
		output.WriteString(key)
		output.WriteByte('=')
		output.WriteString(shellQuote(values[key]))
		output.WriteByte('\n')
	}
	return []byte(output.String())
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func skipSpaceAndComments(input string, index int) int {
	for index < len(input) {
		index = skipHorizontalSpace(input, index)
		if index >= len(input) {
			return index
		}
		if input[index] == '\n' {
			index++
			continue
		}
		if input[index] != '#' {
			return index
		}
		for index < len(input) && input[index] != '\n' {
			index++
		}
	}
	return index
}

func skipHorizontalSpace(input string, index int) int {
	for index < len(input) && (input[index] == ' ' || input[index] == '\t' || input[index] == '\r') {
		index++
	}
	return index
}

func isEnvKeyByte(char byte, first bool) bool {
	if char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' {
		return true
	}
	return !first && char >= '0' && char <= '9'
}

func lineNumber(input string, index int) int {
	if index > len(input) {
		index = len(input)
	}
	return 1 + strings.Count(input[:index], "\n")
}
