package truncation

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	incompleteTailSymbolRE = regexp.MustCompile(`(?:[,:\[{(]|[，：［【（｛])\s*$`)
	terminatorRE           = regexp.MustCompile(`(?:\.{3}|[.!?;。！？；…])(?:["'”’)\]）】}」』》〉〕］｝＞>]+)?$`)
)

func ShouldContinue(text string, plainText bool, minChars int) bool {
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	if trimmed == "" {
		return false
	}
	if HasUnclosedFence(trimmed) {
		return true
	}
	if incompleteTailSymbolRE.MatchString(trimmed) {
		return true
	}
	if EndsWithPartialTag(trimmed) || EndsWithOpeningTag(trimmed) {
		return true
	}
	tail := TailWindow(trimmed, 400, 6)
	if HasUnclosedDelimiters(tail) || HasIncompleteMarkdownTail(tail) {
		return true
	}
	if !plainText {
		return false
	}
	if minChars <= 0 {
		minChars = 120
	}
	if utf8.RuneCountInString(trimmed) < minChars {
		return false
	}
	last := LastNonEmptyLine(trimmed)
	if last == "" || terminatorRE.MatchString(last) {
		return false
	}
	return EndsWithWordish(last)
}

func DeduplicateContinuation(existing, continuation string) string {
	if existing == "" || continuation == "" {
		return continuation
	}
	maxOverlap := min(500, len(existing), len(continuation))
	if maxOverlap < 10 {
		return continuation
	}
	tail := existing[len(existing)-maxOverlap:]
	for n := maxOverlap; n >= 10; n-- {
		if strings.HasSuffix(tail, continuation[:n]) {
			return continuation[n:]
		}
	}

	contLines := strings.Split(continuation, "\n")
	tailLines := strings.Split(tail, "\n")
	if len(contLines) == 0 || len(tailLines) == 0 {
		return continuation
	}
	first := strings.TrimSpace(contLines[0])
	if utf8.RuneCountInString(first) < 10 {
		return continuation
	}
	for i := len(tailLines) - 1; i >= 0; i-- {
		if strings.TrimSpace(tailLines[i]) != first {
			continue
		}
		matched := 1
		for k := 1; k < len(contLines) && i+k < len(tailLines); k++ {
			if strings.TrimSpace(contLines[k]) != strings.TrimSpace(tailLines[i+k]) {
				break
			}
			matched++
		}
		if matched >= 2 {
			return strings.Join(contLines[matched:], "\n")
		}
		break
	}
	return continuation
}

func HasUnclosedFence(text string) bool {
	for i := 0; i < len(text); i++ {
		marker := fenceMarkerAt(text, i)
		if marker == "" {
			continue
		}
		end, closed := skipFence(text, i, marker)
		if !closed {
			return true
		}
		i = max(i, end-1)
	}
	return false
}

func fenceMarkerAt(text string, i int) string {
	if i > 0 && text[i-1] != '\n' {
		return ""
	}
	ch := text[i]
	if ch != '`' && ch != '~' {
		return ""
	}
	end := i
	for end < len(text) && text[end] == ch {
		end++
	}
	if end-i < 3 {
		return ""
	}
	return text[i:end]
}

func skipFence(text string, i int, marker string) (end int, closed bool) {
	openLineEnd := strings.IndexByte(text[i:], '\n')
	if openLineEnd < 0 {
		return len(text), false
	}
	searchFrom := i + openLineEnd + 1
	ch := marker[0]
	need := len(marker)
	for searchFrom < len(text) {
		lineEnd := strings.IndexByte(text[searchFrom:], '\n')
		line := text[searchFrom:]
		if lineEnd >= 0 {
			line = text[searchFrom : searchFrom+lineEnd]
		}
		run := 0
		for run < len(line) && line[run] == ch {
			run++
		}
		if run >= need {
			if lineEnd < 0 {
				return len(text), true
			}
			return searchFrom + lineEnd + 1, true
		}
		if lineEnd < 0 {
			break
		}
		searchFrom += lineEnd + 1
	}
	return len(text), false
}

func EndsWithPartialTag(text string) bool {
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	open := strings.LastIndex(trimmed, "<")
	if open < 0 || strings.Contains(trimmed[open:], ">") {
		return false
	}
	segment := strings.TrimSpace(trimmed[open+1:])
	if segment == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(segment)
	return unicode.IsLetter(r) || r == '/'
}

func EndsWithOpeningTag(text string) bool {
	trimmed := strings.TrimRightFunc(text, unicode.IsSpace)
	open := strings.LastIndex(trimmed, "<")
	if open < 0 || !strings.HasSuffix(trimmed, ">") {
		return false
	}
	segment := strings.TrimSpace(trimmed[open+1 : len(trimmed)-1])
	if segment == "" || strings.HasPrefix(segment, "/") || strings.HasSuffix(segment, "/") {
		return false
	}
	name := strings.Fields(segment)[0]
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsLetter(r)
}

func TailWindow(text string, maxChars, maxLines int) string {
	if maxChars <= 0 || utf8.RuneCountInString(text) <= maxChars {
		return text
	}
	runes := []rune(text)
	start := len(runes) - maxChars
	newlines := 0
	for i := len(runes) - 1; i >= start; i-- {
		if runes[i] != '\n' {
			continue
		}
		newlines++
		if newlines >= maxLines {
			start = i + 1
			break
		}
	}
	return string(runes[start:])
}

func HasIncompleteMarkdownTail(text string) bool {
	last := LastNonEmptyLine(text)
	if last == "" {
		return false
	}
	if last == "|" {
		return true
	}
	if strings.HasPrefix(last, "|") && strings.Count(last, "|") < 3 {
		return true
	}
	if regexp.MustCompile(`^(?:[-*+]\s*|\d+\.\s*|>\s*|-\s\[[ xX]?\]\s*)$`).MatchString(last) {
		return true
	}
	return false
}

func HasUnclosedDelimiters(text string) bool {
	stack := make([]rune, 0, 8)
	inlineTicks := 0
	pairs := map[rune]rune{
		'(': ')', '[': ']', '{': '}',
		'（': '）', '【': '】', '［': '］', '｛': '｝',
		'“': '”', '‘': '’', '「': '」', '『': '』', '《': '》',
	}
	closers := map[rune]bool{}
	for _, close := range pairs {
		closers[close] = true
	}
	var prev rune
	for _, r := range text {
		if r == '`' && prev != '\\' {
			inlineTicks ^= 1
			prev = r
			continue
		}
		if close, ok := pairs[r]; ok {
			stack = append(stack, close)
			prev = r
			continue
		}
		if closers[r] && len(stack) > 0 && stack[len(stack)-1] == r {
			stack = stack[:len(stack)-1]
		}
		prev = r
	}
	return inlineTicks != 0 || len(stack) > 0
}

func LastNonEmptyLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}

func EndsWithWordish(text string) bool {
	text = strings.TrimRight(text, `"'”’)]）】}」』》〉〕］｝＞> `+"\t\r\n")
	if text == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text)
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '%' || r == '/' || r == '\\' || r == '-'
}
