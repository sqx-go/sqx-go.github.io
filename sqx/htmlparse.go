// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"html"
	"net/url"
	"strings"
	"unicode"
)

const markupDeclarationTag = "!"

var (
	rawTextTags          = map[string]bool{"script": true, "style": true}
	invisibleContentTags = map[string]bool{"script": true, "style": true, "noscript": true}
	urlValuedAttributes  = map[string]bool{"src": true, "href": true, "action": true}
)

type htmlTag struct {
	Name          string
	Attributes    map[string]string
	IsClosing     bool
	IsSelfClosing bool
	Start         int
	End           int
}

func (tag htmlTag) opens(name string) bool {
	return !tag.IsClosing && tag.Name == name
}

func extractPlainText(document string) string {
	var text strings.Builder
	textStart := 0
	hiddenDepth := 0
	for _, tag := range scanHTMLTags(document) {
		if hiddenDepth == 0 && tag.Start > textStart {
			text.WriteString(html.UnescapeString(document[textStart:tag.Start]))
			text.WriteByte(' ')
		}
		hiddenDepth = updateHiddenDepth(hiddenDepth, tag)
		textStart = tag.End
	}
	if hiddenDepth == 0 && textStart < len(document) {
		text.WriteString(html.UnescapeString(document[textStart:]))
	}
	return normalizeWhitespace(text.String())
}

func extractTagTexts(document, tagName string) []string {
	tagName = strings.ToLower(strings.TrimSpace(tagName))
	tags := scanHTMLTags(document)
	var texts []string
	for index, tag := range tags {
		if !tag.opens(tagName) || tag.IsSelfClosing {
			continue
		}
		closing, found := findMatchingClose(tags[index+1:], tagName)
		if !found {
			continue
		}
		if text := extractPlainText(document[tag.End:closing.Start]); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

func extractMetaContent(document, key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, tag := range scanHTMLTags(document) {
		if !tag.opens("meta") {
			continue
		}
		for _, attribute := range []string{"name", "property", "http-equiv"} {
			if strings.ToLower(tag.Attributes[attribute]) == key {
				return strings.TrimSpace(tag.Attributes["content"])
			}
		}
	}
	return ""
}

func extractLinks(document, baseURL string) []string {
	base := parseBaseURL(baseURL)
	seen := map[string]bool{}
	var links []string
	for _, tag := range scanHTMLTags(document) {
		if !tag.opens("a") {
			continue
		}
		href := strings.TrimSpace(tag.Attributes["href"])
		if isNonNavigableHref(href) {
			continue
		}
		href = resolveAgainst(base, href)
		if !seen[href] {
			seen[href] = true
			links = append(links, href)
		}
	}
	return links
}

func extractAttributeValues(document, tagName, attributeName, baseURL string) []string {
	tagName = strings.ToLower(strings.TrimSpace(tagName))
	attributeName = strings.ToLower(strings.TrimSpace(attributeName))
	var base *url.URL
	if urlValuedAttributes[attributeName] {
		base = parseBaseURL(baseURL)
	}
	var values []string
	for _, tag := range scanHTMLTags(document) {
		if !tag.opens(tagName) {
			continue
		}
		if value := tag.Attributes[attributeName]; value != "" {
			values = append(values, resolveAgainst(base, value))
		}
	}
	return values
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func updateHiddenDepth(depth int, tag htmlTag) int {
	if !invisibleContentTags[tag.Name] {
		return depth
	}
	if tag.IsClosing {
		return max(depth-1, 0)
	}
	if tag.IsSelfClosing {
		return depth
	}
	return depth + 1
}

func findMatchingClose(tags []htmlTag, tagName string) (htmlTag, bool) {
	depth := 1
	for _, tag := range tags {
		if tag.Name != tagName {
			continue
		}
		switch {
		case tag.IsClosing:
			depth--
		case !tag.IsSelfClosing:
			depth++
		}
		if depth == 0 {
			return tag, true
		}
	}
	return htmlTag{}, false
}

func isNonNavigableHref(href string) bool {
	return href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(strings.ToLower(href), "javascript:")
}

func parseBaseURL(baseURL string) *url.URL {
	if baseURL == "" {
		return nil
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	return base
}

func resolveAgainst(base *url.URL, reference string) string {
	if base == nil {
		return reference
	}
	parsed, err := url.Parse(reference)
	if err != nil {
		return reference
	}
	return base.ResolveReference(parsed).String()
}

func scanHTMLTags(document string) []htmlTag {
	var tags []htmlTag
	position := 0
	for position < len(document) {
		openOffset := strings.IndexByte(document[position:], '<')
		if openOffset == -1 {
			break
		}
		start := position + openOffset
		tag, found := scanTagAt(document, start)
		if !found {
			position = start + 1
			continue
		}
		tags = append(tags, tag)
		position = tag.End
		if rawTextTags[tag.Name] && !tag.IsClosing && !tag.IsSelfClosing {
			if closing, closed := findRawTextClose(document, tag.End, tag.Name); closed {
				tags = append(tags, closing)
				position = closing.End
			}
		}
	}
	return tags
}

func scanTagAt(document string, start int) (htmlTag, bool) {
	rest := document[start:]
	switch {
	case strings.HasPrefix(rest, "<!--"):
		return scanDeclaration(document, start, "-->"), true
	case strings.HasPrefix(rest, "<!"), strings.HasPrefix(rest, "<?"):
		return scanDeclaration(document, start, ">"), true
	}
	if !startsTagName(rest[1:]) {
		return htmlTag{}, false
	}
	end, found := findTagEnd(document, start+1)
	if !found {
		return htmlTag{}, false
	}
	tag := parseTag(document[start+1:end-1], start, end)
	return tag, tag.Name != ""
}

func startsTagName(text string) bool {
	text = strings.TrimPrefix(text, "/")
	return text != "" && isASCIILetter(text[0])
}

func isASCIILetter(character byte) bool {
	return ('a' <= character && character <= 'z') || ('A' <= character && character <= 'Z')
}

func scanDeclaration(document string, start int, terminator string) htmlTag {
	end := len(document)
	if offset := strings.Index(document[start+2:], terminator); offset != -1 {
		end = start + 2 + offset + len(terminator)
	}
	return htmlTag{Name: markupDeclarationTag, Start: start, End: end}
}

func findTagEnd(document string, position int) (int, bool) {
	var quote byte
	for ; position < len(document); position++ {
		character := document[position]
		switch {
		case quote != 0:
			if character == quote {
				quote = 0
			}
		case character == '"' || character == '\'':
			quote = character
		case character == '>':
			return position + 1, true
		}
	}
	return 0, false
}

func findRawTextClose(document string, position int, tagName string) (htmlTag, bool) {
	closeOffset := strings.Index(strings.ToLower(document[position:]), "</"+tagName)
	if closeOffset == -1 {
		return htmlTag{}, false
	}
	closeStart := position + closeOffset
	endOffset := strings.IndexByte(document[closeStart:], '>')
	if endOffset == -1 {
		return htmlTag{}, false
	}
	return htmlTag{Name: tagName, IsClosing: true, Start: closeStart, End: closeStart + endOffset + 1}, true
}

func parseTag(rawTag string, start, end int) htmlTag {
	tag := htmlTag{Attributes: map[string]string{}, Start: start, End: end}
	rawTag = strings.TrimSpace(rawTag)
	if rest, isClosing := strings.CutPrefix(rawTag, "/"); isClosing {
		tag.IsClosing = true
		rawTag = strings.TrimSpace(rest)
	}
	if rest, isSelfClosing := strings.CutSuffix(rawTag, "/"); isSelfClosing {
		tag.IsSelfClosing = true
		rawTag = strings.TrimSpace(rest)
	}
	nameEnd := strings.IndexFunc(rawTag, func(character rune) bool {
		return unicode.IsSpace(character) || character == '/'
	})
	if nameEnd == -1 {
		nameEnd = len(rawTag)
	}
	tag.Name = strings.ToLower(rawTag[:nameEnd])
	if !tag.IsClosing {
		tag.Attributes = parseAttributes(rawTag[nameEnd:])
	}
	return tag
}

func parseAttributes(text string) map[string]string {
	attributes := map[string]string{}
	scanner := attributeScanner{text: text}
	for {
		name, value, found := scanner.next()
		if !found {
			return attributes
		}
		if name != "" {
			attributes[name] = html.UnescapeString(value)
		}
	}
}

type attributeScanner struct {
	text     string
	position int
}

func (scanner *attributeScanner) next() (name, value string, found bool) {
	scanner.skipSpace()
	if scanner.atEnd() {
		return "", "", false
	}
	name = strings.ToLower(scanner.readUntil(func(character byte) bool {
		return isSpaceByte(character) || character == '='
	}))
	scanner.skipSpace()
	if scanner.atEnd() || scanner.text[scanner.position] != '=' {
		return name, "", true
	}
	scanner.position++
	scanner.skipSpace()
	return name, scanner.readValue(), true
}

func (scanner *attributeScanner) readValue() string {
	if scanner.atEnd() {
		return ""
	}
	quote := scanner.text[scanner.position]
	if quote != '"' && quote != '\'' {
		return scanner.readUntil(isSpaceByte)
	}
	scanner.position++
	value := scanner.readUntil(func(character byte) bool { return character == quote })
	if !scanner.atEnd() {
		scanner.position++
	}
	return value
}

func (scanner *attributeScanner) readUntil(isStop func(byte) bool) string {
	start := scanner.position
	for !scanner.atEnd() && !isStop(scanner.text[scanner.position]) {
		scanner.position++
	}
	return scanner.text[start:scanner.position]
}

func (scanner *attributeScanner) skipSpace() {
	for !scanner.atEnd() && isSpaceByte(scanner.text[scanner.position]) {
		scanner.position++
	}
}

func (scanner *attributeScanner) atEnd() bool {
	return scanner.position >= len(scanner.text)
}

func isSpaceByte(character byte) bool {
	return unicode.IsSpace(rune(character))
}
