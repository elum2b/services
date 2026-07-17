package sql

import (
	"fmt"
	"strings"
)

// SplitStatements separates PostgreSQL statements without splitting semicolons
// inside quoted literals, comments, or dollar-quoted function bodies.
func SplitStatements(raw string) ([]string, error) {
	const (
		statePlain = iota
		stateSingleQuote
		stateDoubleQuote
		stateLineComment
		stateBlockComment
		stateDollarQuote
	)

	state := statePlain
	blockDepth := 0
	dollarQuote := ""
	start := 0
	result := make([]string, 0)

	for index := 0; index < len(raw); index++ {
		switch state {
		case statePlain:
			switch raw[index] {
			case '\'':
				state = stateSingleQuote
			case '"':
				state = stateDoubleQuote
			case '-':
				if index+1 < len(raw) && raw[index+1] == '-' {
					state = stateLineComment
					index++
				}
			case '/':
				if index+1 < len(raw) && raw[index+1] == '*' {
					state = stateBlockComment
					blockDepth = 1
					index++
				}
			case '$':
				if delimiter, ok := dollarQuoteDelimiter(raw[index:]); ok {
					state = stateDollarQuote
					dollarQuote = delimiter
					index += len(delimiter) - 1
				}
			case ';':
				if statement := strings.TrimSpace(raw[start:index]); statement != "" {
					result = append(result, statement)
				}
				start = index + 1
			}
		case stateSingleQuote:
			if raw[index] != '\'' {
				continue
			}
			if index+1 < len(raw) && raw[index+1] == '\'' {
				index++
				continue
			}
			state = statePlain
		case stateDoubleQuote:
			if raw[index] != '"' {
				continue
			}
			if index+1 < len(raw) && raw[index+1] == '"' {
				index++
				continue
			}
			state = statePlain
		case stateLineComment:
			if raw[index] == '\n' {
				state = statePlain
			}
		case stateBlockComment:
			if index+1 >= len(raw) {
				continue
			}
			switch raw[index : index+2] {
			case "/*":
				blockDepth++
				index++
			case "*/":
				blockDepth--
				index++
				if blockDepth == 0 {
					state = statePlain
				}
			}
		case stateDollarQuote:
			if strings.HasPrefix(raw[index:], dollarQuote) {
				index += len(dollarQuote) - 1
				state = statePlain
			}
		}
	}

	switch state {
	case stateSingleQuote:
		return nil, fmt.Errorf("unterminated single-quoted SQL string")
	case stateDoubleQuote:
		return nil, fmt.Errorf("unterminated double-quoted SQL identifier")
	case stateBlockComment:
		return nil, fmt.Errorf("unterminated SQL block comment")
	case stateDollarQuote:
		return nil, fmt.Errorf("unterminated SQL dollar quote %q", dollarQuote)
	}

	if statement := strings.TrimSpace(raw[start:]); statement != "" {
		result = append(result, statement)
	}
	return result, nil
}

func dollarQuoteDelimiter(value string) (string, bool) {
	if len(value) < 2 || value[0] != '$' {
		return "", false
	}
	if value[1] == '$' {
		return "$$", true
	}
	if !isDollarQuoteIdentifierStart(value[1]) {
		return "", false
	}
	for index := 2; index < len(value); index++ {
		if value[index] == '$' {
			return value[:index+1], true
		}
		if !isDollarQuoteIdentifierPart(value[index]) {
			return "", false
		}
	}
	return "", false
}

func isDollarQuoteIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isDollarQuoteIdentifierPart(value byte) bool {
	return isDollarQuoteIdentifierStart(value) || value >= '0' && value <= '9'
}
