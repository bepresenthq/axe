package build

import (
	"fmt"
	"strings"
	"unicode"
)

func appendUnique(paths []string, value string) []string {
	if value == "" {
		return paths
	}
	for _, p := range paths {
		if p == value {
			return paths
		}
	}
	return append(paths, value)
}

// Response files use shell-style quoting, but are data: never execute them.
func responseArguments(data string) ([]string, error) {
	var args []string
	var token strings.Builder
	var quote rune
	escaped, started := false, false
	for _, c := range data {
		if escaped {
			token.WriteRune(c)
			escaped = false
			started = true
			continue
		}
		if c == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				token.WriteRune(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			started = true
			continue
		}
		if unicode.IsSpace(c) {
			if started {
				args = append(args, token.String())
				token.Reset()
				started = false
			}
			continue
		}
		token.WriteRune(c)
		started = true
	}
	if escaped || quote != 0 {
		return nil, fmt.Errorf("unterminated quote or escape")
	}
	if started {
		args = append(args, token.String())
	}
	return args, nil
}
