package rag

import (
	"strings"
	"unicode"
)

// Tokenize 简单分词：小写 + 去标点 + 按空格/unicode 分词
func Tokenize(text string) []string {
	text = strings.ToLower(text)
	var terms []string
	var buf strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			buf.WriteRune(r)
		} else {
			if buf.Len() > 0 {
				terms = append(terms, buf.String())
				buf.Reset()
			}
		}
	}
	if buf.Len() > 0 {
		terms = append(terms, buf.String())
	}
	return terms
}
