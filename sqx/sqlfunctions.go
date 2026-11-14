// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"modernc.org/sqlite"
)

type sqlFunction struct {
	name          string
	argumentCount int32
	deterministic bool
	evaluate      func(arguments []driver.Value) (driver.Value, error)
}

var (
	sessionFetcher  = &WebFetcher{settings: FetchSettings{Offline: true}}
	compiledRegexes sync.Map
)

func init() {
	for _, function := range sqlFunctions() {
		register(function)
	}
}

func useWebFetcher(fetcher *WebFetcher) {
	sessionFetcher = fetcher
}

func sqlFunctions() []sqlFunction {
	var functions []sqlFunction
	functions = append(functions, urlFunctions()...)
	functions = append(functions, httpFunctions()...)
	functions = append(functions, htmlFunctions()...)
	functions = append(functions, networkFunctions()...)
	functions = append(functions, textFunctions()...)
	return functions
}

func register(function sqlFunction) {
	evaluate := function.evaluate
	implementation := func(_ *sqlite.FunctionContext, arguments []driver.Value) (driver.Value, error) {
		return evaluate(arguments)
	}
	if function.deterministic {
		sqlite.MustRegisterDeterministicScalarFunction(function.name, function.argumentCount, implementation)
		return
	}
	sqlite.MustRegisterScalarFunction(function.name, function.argumentCount, implementation)
}

func urlFunctions() []sqlFunction {
	return []sqlFunction{
		{"url_host", 1, true, evaluateURLHost},
		{"url_domain", 1, true, evaluateURLHost},
		{"url_scheme", 1, true, evaluateURLScheme},
		{"url_path", 1, true, evaluateURLPath},
		{"url_query", 2, true, evaluateURLQuery},
		{"url_ext", 1, true, evaluateURLExtension},
		{"url_norm", 1, true, evaluateURLNormalized},
		{"url_join", 2, true, evaluateURLJoin},
	}
}

func httpFunctions() []sqlFunction {
	return []sqlFunction{
		{"http_status", 1, false, fetchedField(int64(0), func(response *HTTPResponse) driver.Value { return int64(response.StatusCode) })},
		{"http_body", 1, false, fetchedField(nil, func(response *HTTPResponse) driver.Value { return response.Body })},
		{"http_time_ms", 1, false, fetchedField(int64(0), func(response *HTTPResponse) driver.Value { return response.ResponseTimeMS })},
		{"http_final_url", 1, false, fetchedField(nil, func(response *HTTPResponse) driver.Value { return nullIfEmpty(response.FinalURL) })},
		{"http_size", 1, false, fetchedField(int64(0), func(response *HTTPResponse) driver.Value { return int64(len(response.Body)) })},
		{"http_error", 1, false, fetchedField(nil, func(response *HTTPResponse) driver.Value { return nullIfEmpty(response.Error) })},
		{"http_headers_json", 1, false, fetchedField("{}", headersJSON)},
		{"http_header", 2, false, evaluateHTTPHeader},
	}
}

func htmlFunctions() []sqlFunction {
	return []sqlFunction{
		{"html_title", 1, false, evaluateHTMLTitle},
		{"html_meta", 2, false, evaluateHTMLMeta},
		{"html_text", 1, false, evaluateHTMLText},
		{"html_links", 1, false, evaluateHTMLLinks},
		{"html_tag_texts", 2, false, evaluateHTMLTagTexts},
		{"html_attr", 3, false, evaluateHTMLAttribute},
	}
}

func networkFunctions() []sqlFunction {
	return []sqlFunction{
		{"dns_ip", 1, false, evaluateDNSFirstAddress},
		{"dns_ips_json", 1, false, evaluateDNSAddresses},
		{"dns_txt_json", 1, false, evaluateDNSTXTRecords},
		{"ip_is_private", 1, true, evaluateIPIsPrivate},
	}
}

func textFunctions() []sqlFunction {
	return []sqlFunction{
		{"regexp", 2, true, evaluateRegexpOperator},
		{"re_match", 2, true, evaluateRegexMatch},
		{"re_extract", 3, true, evaluateRegexExtract},
		{"re_extract_all", 2, true, evaluateRegexExtractAll},
		{"shannon_entropy", 1, true, evaluateShannonEntropy},
		{"sha256", 1, true, evaluateSHA256},
	}
}

func evaluateURLHost(arguments []driver.Value) (driver.Value, error) {
	parsed, ok := parseURLAssumingHTTPS(textArgument(arguments[0]))
	if !ok {
		return "", nil
	}
	return strings.ToLower(parsed.Hostname()), nil
}

func evaluateURLScheme(arguments []driver.Value) (driver.Value, error) {
	parsed, err := url.Parse(textArgument(arguments[0]))
	if err != nil {
		return "", nil
	}
	return strings.ToLower(parsed.Scheme), nil
}

func evaluateURLPath(arguments []driver.Value) (driver.Value, error) {
	parsed, ok := parseURLAssumingHTTPS(textArgument(arguments[0]))
	if !ok {
		return "", nil
	}
	if parsed.Path == "" {
		return "/", nil
	}
	return parsed.Path, nil
}

func evaluateURLQuery(arguments []driver.Value) (driver.Value, error) {
	parsed, ok := parseURLAssumingHTTPS(textArgument(arguments[0]))
	if !ok {
		return nil, nil
	}
	return nullIfEmpty(parsed.Query().Get(textArgument(arguments[1]))), nil
}

func evaluateURLExtension(arguments []driver.Value) (driver.Value, error) {
	parsed, ok := parseURLAssumingHTTPS(textArgument(arguments[0]))
	if !ok {
		return "", nil
	}
	return strings.ToLower(path.Ext(parsed.Path)), nil
}

func evaluateURLNormalized(arguments []driver.Value) (driver.Value, error) {
	raw := strings.TrimSpace(textArgument(arguments[0]))
	parsed, ok := parseURLAssumingHTTPS(raw)
	if !ok {
		return raw, nil
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.RawQuery = parsed.Query().Encode()
	return parsed.String(), nil
}

func evaluateURLJoin(arguments []driver.Value) (driver.Value, error) {
	reference := textArgument(arguments[1])
	base, err := url.Parse(textArgument(arguments[0]))
	if err != nil {
		return reference, nil
	}
	return resolveAgainst(base, reference), nil
}

func fetchedField(emptyValue driver.Value, read func(*HTTPResponse) driver.Value) func([]driver.Value) (driver.Value, error) {
	return func(arguments []driver.Value) (driver.Value, error) {
		target := textArgument(arguments[0])
		if target == "" {
			return emptyValue, nil
		}
		return read(sessionFetcher.Fetch(target)), nil
	}
}

func headersJSON(response *HTTPResponse) driver.Value {
	if response.Headers == nil {
		return "{}"
	}
	return jsonText(response.Headers)
}

func evaluateHTTPHeader(arguments []driver.Value) (driver.Value, error) {
	target := textArgument(arguments[0])
	headerName := strings.ToLower(strings.TrimSpace(textArgument(arguments[1])))
	if target == "" || headerName == "" {
		return nil, nil
	}
	values := sessionFetcher.Fetch(target).Headers[headerName]
	if len(values) == 0 {
		return nil, nil
	}
	return strings.Join(values, ", "), nil
}

func evaluateHTMLTitle(arguments []driver.Value) (driver.Value, error) {
	document, _ := resolveHTMLDocument(textArgument(arguments[0]))
	titles := extractTagTexts(document, "title")
	if len(titles) == 0 {
		return nil, nil
	}
	return titles[0], nil
}

func evaluateHTMLMeta(arguments []driver.Value) (driver.Value, error) {
	document, _ := resolveHTMLDocument(textArgument(arguments[0]))
	return nullIfEmpty(extractMetaContent(document, textArgument(arguments[1]))), nil
}

func evaluateHTMLText(arguments []driver.Value) (driver.Value, error) {
	document, _ := resolveHTMLDocument(textArgument(arguments[0]))
	return extractPlainText(document), nil
}

func evaluateHTMLLinks(arguments []driver.Value) (driver.Value, error) {
	document, baseURL := resolveHTMLDocument(textArgument(arguments[0]))
	return jsonArray(extractLinks(document, baseURL)), nil
}

func evaluateHTMLTagTexts(arguments []driver.Value) (driver.Value, error) {
	document, _ := resolveHTMLDocument(textArgument(arguments[0]))
	return jsonArray(extractTagTexts(document, textArgument(arguments[1]))), nil
}

func evaluateHTMLAttribute(arguments []driver.Value) (driver.Value, error) {
	document, baseURL := resolveHTMLDocument(textArgument(arguments[0]))
	values := extractAttributeValues(document, textArgument(arguments[1]), textArgument(arguments[2]), baseURL)
	return jsonArray(values), nil
}

func evaluateDNSFirstAddress(arguments []driver.Value) (driver.Value, error) {
	addresses := sessionFetcher.LookupAddresses(textArgument(arguments[0]))
	if len(addresses) == 0 {
		return nil, nil
	}
	return addresses[0], nil
}

func evaluateDNSAddresses(arguments []driver.Value) (driver.Value, error) {
	return jsonArray(sessionFetcher.LookupAddresses(textArgument(arguments[0]))), nil
}

func evaluateDNSTXTRecords(arguments []driver.Value) (driver.Value, error) {
	return jsonArray(sessionFetcher.LookupTXTRecords(textArgument(arguments[0]))), nil
}

func evaluateIPIsPrivate(arguments []driver.Value) (driver.Value, error) {
	address := net.ParseIP(strings.TrimSpace(textArgument(arguments[0])))
	if address == nil {
		return int64(0), nil
	}
	isPrivate := address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast()
	return sqlBoolean(isPrivate), nil
}

func evaluateRegexpOperator(arguments []driver.Value) (driver.Value, error) {
	return matchRegex(textArgument(arguments[1]), textArgument(arguments[0]))
}

func evaluateRegexMatch(arguments []driver.Value) (driver.Value, error) {
	return matchRegex(textArgument(arguments[0]), textArgument(arguments[1]))
}

func matchRegex(text, pattern string) (driver.Value, error) {
	expression, err := compileRegex(pattern)
	if err != nil {
		return int64(0), err
	}
	return sqlBoolean(expression.MatchString(text)), nil
}

func evaluateRegexExtract(arguments []driver.Value) (driver.Value, error) {
	expression, err := compileRegex(textArgument(arguments[1]))
	if err != nil {
		return nil, err
	}
	groupIndex := integerArgument(arguments[2])
	matches := expression.FindStringSubmatch(textArgument(arguments[0]))
	if groupIndex < 0 || groupIndex >= len(matches) {
		return nil, nil
	}
	return matches[groupIndex], nil
}

func evaluateRegexExtractAll(arguments []driver.Value) (driver.Value, error) {
	expression, err := compileRegex(textArgument(arguments[1]))
	if err != nil {
		return "[]", err
	}
	return jsonArray(expression.FindAllString(textArgument(arguments[0]), -1)), nil
}

func evaluateShannonEntropy(arguments []driver.Value) (driver.Value, error) {
	return shannonEntropy(textArgument(arguments[0])), nil
}

func evaluateSHA256(arguments []driver.Value) (driver.Value, error) {
	digest := sha256.Sum256([]byte(textArgument(arguments[0])))
	return hex.EncodeToString(digest[:]), nil
}

func shannonEntropy(text string) float64 {
	frequencies := map[rune]float64{}
	total := 0.0
	for _, character := range text {
		frequencies[character]++
		total++
	}
	entropy := 0.0
	for _, count := range frequencies {
		probability := count / total
		entropy -= probability * math.Log2(probability)
	}
	return entropy
}

func resolveHTMLDocument(input string) (document, baseURL string) {
	if !hasHTTPScheme(input) {
		return input, ""
	}
	response := sessionFetcher.Fetch(input)
	return response.Body, response.FinalURL
}

func parseURLAssumingHTTPS(raw string) (*url.URL, bool) {
	if raw == "" {
		return nil, false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	return parsed, err == nil
}

func compileRegex(pattern string) (*regexp.Regexp, error) {
	if cached, found := compiledRegexes.Load(pattern); found {
		return cached.(*regexp.Regexp), nil
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	compiledRegexes.Store(pattern, expression)
	return expression, nil
}

func textArgument(value driver.Value) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

func integerArgument(value driver.Value) int {
	switch typed := value.(type) {
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		number, _ := strconv.Atoi(strings.TrimSpace(typed))
		return number
	default:
		return 0
	}
}

func nullIfEmpty(text string) driver.Value {
	if text == "" {
		return nil
	}
	return text
}

func sqlBoolean(condition bool) int64 {
	if condition {
		return 1
	}
	return 0
}

func jsonArray(values []string) string {
	if values == nil {
		values = []string{}
	}
	return jsonText(values)
}

func jsonText(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
