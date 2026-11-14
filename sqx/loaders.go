// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	formatAuto      = "auto"
	formatCSV       = "csv"
	formatTSV       = "tsv"
	formatJSONLines = "jsonl"
	formatJSON      = "json"
	formatLines     = "lines"

	sqlTypeInteger = "INTEGER"
	sqlTypeReal    = "REAL"
	sqlTypeText    = "TEXT"
	sqlTypeAny     = ""

	typeInferenceSampleRows  = 100
	formatDetectionPeekBytes = 4096
	readBufferBytes          = 64 * 1024
	initialLineBufferBytes   = 1024 * 1024
	maxLineBytes             = 20 * 1024 * 1024
)

var (
	byteOrderMark       = []byte("\uFEFF")
	nonIdentifierRunes  = regexp.MustCompile(`[^a-zA-Z0-9_]+`)
	plainIdentifier     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	missingValueMarkers = map[string]bool{"": true, "null": true, "n/a": true, "na": true, "nan": true, "none": true, "-": true}
	formatAliases       = map[string]string{
		"auto": formatAuto, "csv": formatCSV, "tsv": formatTSV, "tab": formatTSV,
		"jsonl": formatJSONLines, "ndjson": formatJSONLines, "json": formatJSON,
		"lines": formatLines, "line": formatLines, "raw": formatLines, "txt": formatLines,
	}
	extensionFormats = map[string]string{
		".csv": formatCSV, ".tsv": formatTSV, ".tab": formatTSV,
		".jsonl": formatJSONLines, ".ndjson": formatJSONLines, ".json": formatJSON,
	}
)

type TableSource struct {
	Reader    io.Reader
	TableName string
	Format    string
	HasHeader bool
	Temporary bool
}

type columnDefinition struct {
	Name    string
	SQLType string
}

type tableTarget struct {
	transaction *sql.Tx
	name        string
	temporary   bool
}

func loadTable(database *sql.DB, source TableSource) error {
	reader := bufio.NewReaderSize(source.Reader, readBufferBytes)
	preview, _ := reader.Peek(formatDetectionPeekBytes)
	if bytes.HasPrefix(preview, byteOrderMark) {
		_, _ = reader.Discard(len(byteOrderMark))
		preview = preview[len(byteOrderMark):]
	}
	if len(bytes.TrimSpace(preview)) == 0 {
		return nil
	}
	format, err := resolveInputFormat(source.Format, preview)
	if err != nil {
		return err
	}

	transaction, err := database.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	table := tableTarget{transaction: transaction, name: source.TableName, temporary: source.Temporary}
	if err := loadFormat(table, reader, format, source.HasHeader); err != nil {
		return fmt.Errorf("loading table %s: %w", source.TableName, err)
	}
	return transaction.Commit()
}

func formatFromExtension(path string) string {
	if format, known := extensionFormats[strings.ToLower(filepath.Ext(path))]; known {
		return format
	}
	return formatAuto
}

func tableNameFromPath(path string) string {
	base := filepath.Base(path)
	return sanitizeIdentifier(strings.TrimSuffix(base, filepath.Ext(base)), 0)
}

func isPlainIdentifier(text string) bool {
	return plainIdentifier.MatchString(text)
}

func sanitizeIdentifier(raw string, position int) string {
	name := strings.TrimPrefix(strings.TrimSpace(raw), "\uFEFF")
	name = strings.Trim(nonIdentifierRunes.ReplaceAllString(name, "_"), "_")
	if name == "" {
		return fmt.Sprintf("c%d", position+1)
	}
	if unicode.IsDigit(rune(name[0])) {
		name = "c_" + name
	}
	return strings.ToLower(name)
}

func resolveInputFormat(hint string, preview []byte) (string, error) {
	format, known := formatAliases[strings.ToLower(strings.TrimSpace(hint))]
	if hint == "" {
		format, known = formatAuto, true
	}
	if !known {
		return "", fmt.Errorf("unknown input format %q (expected auto, csv, tsv, jsonl, json or lines)", hint)
	}
	trimmed := bytes.TrimSpace(preview)
	switch format {
	case formatJSON, formatJSONLines:
		if trimmed[0] == '[' {
			return formatJSON, nil
		}
		return formatJSONLines, nil
	case formatAuto:
		return detectFormat(trimmed), nil
	}
	return format, nil
}

func detectFormat(trimmedPreview []byte) string {
	switch trimmedPreview[0] {
	case '[':
		return formatJSON
	case '{':
		return formatJSONLines
	}
	firstLine, _, _ := strings.Cut(string(trimmedPreview), "\n")
	firstLine = strings.TrimSpace(firstLine)
	if strings.Contains(firstLine, "\t") {
		return formatTSV
	}
	if !strings.Contains(firstLine, ",") && hasHTTPScheme(firstLine) {
		return formatLines
	}
	return formatCSV
}

func loadFormat(table tableTarget, reader io.Reader, format string, hasHeader bool) error {
	switch format {
	case formatLines:
		return loadLines(table, reader)
	case formatJSONLines:
		return loadJSONLines(table, reader)
	case formatJSON:
		return loadJSONArray(table, reader)
	case formatTSV:
		return loadDelimited(table, reader, '\t', hasHeader)
	default:
		return loadDelimited(table, reader, ',', hasHeader)
	}
}

func loadLines(table tableTarget, reader io.Reader) error {
	columns := []columnDefinition{{"linenum", sqlTypeInteger}, {"line", sqlTypeText}, {"url", sqlTypeText}}
	if err := table.ensureColumns(columns); err != nil {
		return err
	}
	insert, err := table.prepareInsert(columnNames(columns))
	if err != nil {
		return err
	}
	defer insert.Close()

	scanner := newLineScanner(reader)
	lineNumber := 0
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lineNumber++
		if _, err := insert.Exec(lineNumber, line, strings.TrimSpace(line)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func loadDelimited(table tableTarget, reader io.Reader, separator rune, hasHeader bool) error {
	records := csv.NewReader(reader)
	records.Comma = separator
	records.LazyQuotes = true
	records.FieldsPerRecord = -1

	header, samples, err := readHeaderAndSamples(records, hasHeader)
	if err != nil || header == nil {
		return err
	}
	columns := inferColumns(header, samples)
	if err := table.ensureColumns(columns); err != nil {
		return err
	}
	insert, err := table.prepareInsert(columnNames(columns))
	if err != nil {
		return err
	}
	defer insert.Close()

	for _, record := range samples {
		if _, err := insert.Exec(cellValues(record, columns)...); err != nil {
			return err
		}
	}
	for {
		record, err := records.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := insert.Exec(cellValues(record, columns)...); err != nil {
			return err
		}
	}
}

func readHeaderAndSamples(records *csv.Reader, hasHeader bool) (header []string, samples [][]string, err error) {
	firstRecord, err := records.Read()
	if errors.Is(err, io.EOF) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading header: %w", err)
	}
	header = firstRecord
	if !hasHeader || isLoneURLHeader(firstRecord) {
		samples = append(samples, firstRecord)
		header = positionalHeader(firstRecord, hasHeader)
	}
	for len(samples) < typeInferenceSampleRows {
		record, err := records.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		samples = append(samples, record)
	}
	return header, samples, nil
}

func isLoneURLHeader(record []string) bool {
	return len(record) == 1 && hasHTTPScheme(record[0])
}

func positionalHeader(firstRecord []string, hasHeader bool) []string {
	if hasHeader {
		return []string{"url"}
	}
	header := make([]string, len(firstRecord))
	for index := range firstRecord {
		header[index] = fmt.Sprintf("c%d", index+1)
	}
	return header
}

func inferColumns(header []string, samples [][]string) []columnDefinition {
	names := uniqueColumnNames(header)
	columns := make([]columnDefinition, len(names))
	for index, name := range names {
		columns[index] = columnDefinition{Name: name, SQLType: inferSQLType(columnSamples(samples, index))}
	}
	return columns
}

func columnSamples(records [][]string, columnIndex int) []string {
	var values []string
	for _, record := range records {
		if columnIndex < len(record) {
			values = append(values, record[columnIndex])
		}
	}
	return values
}

func inferSQLType(samples []string) string {
	hasValue, allIntegers, allNumbers := false, true, true
	for _, sample := range samples {
		value := strings.TrimSpace(sample)
		if isMissingValue(value) {
			continue
		}
		hasValue = true
		if hasSignificantLeadingZero(value) {
			return sqlTypeText
		}
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			allIntegers = false
		}
		if !isFiniteNumber(value) {
			allNumbers = false
		}
	}
	switch {
	case !hasValue || !allNumbers:
		return sqlTypeText
	case allIntegers:
		return sqlTypeInteger
	default:
		return sqlTypeReal
	}
}

func isMissingValue(value string) bool {
	return missingValueMarkers[strings.ToLower(strings.TrimSpace(value))]
}

func hasSignificantLeadingZero(value string) bool {
	digits := strings.TrimLeft(value, "+-")
	return len(digits) > 1 && digits[0] == '0' && digits[1] != '.'
}

func isFiniteNumber(value string) bool {
	number, err := strconv.ParseFloat(value, 64)
	return err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func cellValues(record []string, columns []columnDefinition) []any {
	values := make([]any, len(columns))
	for index, column := range columns {
		if index >= len(record) {
			continue
		}
		values[index] = cellValue(record[index], column.SQLType)
	}
	return values
}

func cellValue(raw, sqlType string) any {
	if raw == "" {
		return nil
	}
	if sqlType != sqlTypeText && isMissingValue(raw) {
		return nil
	}
	return raw
}

func uniqueColumnNames(rawNames []string) []string {
	used := map[string]bool{}
	names := make([]string, len(rawNames))
	for index, raw := range rawNames {
		names[index] = claimUniqueName(sanitizeIdentifier(raw, index), used)
	}
	return names
}

func claimUniqueName(base string, used map[string]bool) string {
	name := base
	for suffix := 2; used[name]; suffix++ {
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
	used[name] = true
	return name
}

func columnNames(columns []columnDefinition) []string {
	names := make([]string, len(columns))
	for index, column := range columns {
		names[index] = column.Name
	}
	return names
}

func newLineScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, initialLineBufferBytes), maxLineBytes)
	return scanner
}

type jsonRowWriter struct {
	table       tableTarget
	columnByKey map[string]string
	keys        []string
	usedNames   map[string]bool
	insert      *sql.Stmt
}

func loadJSONLines(table tableTarget, reader io.Reader) error {
	writer := &jsonRowWriter{table: table, columnByKey: map[string]string{}, usedNames: map[string]bool{}}
	defer writer.close()
	scanner := newLineScanner(reader)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		record, err := decodeJSONObject(json.NewDecoder(bytes.NewReader(line)))
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if err := writer.write(record); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func loadJSONArray(table tableTarget, reader io.Reader) error {
	writer := &jsonRowWriter{table: table, columnByKey: map[string]string{}, usedNames: map[string]bool{}}
	defer writer.close()
	decoder := json.NewDecoder(reader)
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("reading JSON array: %w", err)
	}
	for position := 1; decoder.More(); position++ {
		record, err := decodeJSONObject(decoder)
		if err != nil {
			return fmt.Errorf("array element %d: %w", position, err)
		}
		if err := writer.write(record); err != nil {
			return err
		}
	}
	return nil
}

func decodeJSONObject(decoder *json.Decoder) (map[string]any, error) {
	decoder.UseNumber()
	var record map[string]any
	if err := decoder.Decode(&record); err != nil {
		return nil, fmt.Errorf("expected a JSON object: %w", err)
	}
	return record, nil
}

func (writer *jsonRowWriter) write(record map[string]any) error {
	if err := writer.addNewKeys(record); err != nil {
		return err
	}
	values := make([]any, len(writer.keys))
	for index, key := range writer.keys {
		values[index] = jsonCellValue(record[key])
	}
	_, err := writer.insert.Exec(values...)
	return err
}

func (writer *jsonRowWriter) addNewKeys(record map[string]any) error {
	var newKeys []string
	for key := range record {
		if _, known := writer.columnByKey[key]; !known {
			newKeys = append(newKeys, key)
		}
	}
	if len(newKeys) == 0 && writer.insert != nil {
		return nil
	}
	slices.Sort(newKeys)
	for _, key := range newKeys {
		writer.columnByKey[key] = claimUniqueName(sanitizeIdentifier(key, len(writer.keys)), writer.usedNames)
		writer.keys = append(writer.keys, key)
	}
	return writer.rebuildInsert()
}

func (writer *jsonRowWriter) rebuildInsert() error {
	columns := make([]columnDefinition, len(writer.keys))
	for index, key := range writer.keys {
		columns[index] = columnDefinition{Name: writer.columnByKey[key], SQLType: sqlTypeAny}
	}
	if err := writer.table.ensureColumns(columns); err != nil {
		return err
	}
	writer.close()
	insert, err := writer.table.prepareInsert(columnNames(columns))
	writer.insert = insert
	return err
}

func (writer *jsonRowWriter) close() {
	if writer.insert != nil {
		_ = writer.insert.Close()
		writer.insert = nil
	}
}

func jsonCellValue(value any) any {
	switch typed := value.(type) {
	case nil, string, bool:
		return typed
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}
		number, _ := typed.Float64()
		return number
	default:
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	}
}

func (table tableTarget) qualifiedName() string {
	return table.schema() + "." + quoteIdentifier(table.name)
}

func (table tableTarget) schema() string {
	if table.temporary {
		return "temp"
	}
	return "main"
}

func (table tableTarget) ensureColumns(columns []columnDefinition) error {
	existing, err := table.existingColumns()
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return table.create(columns)
	}
	for _, column := range columns {
		if existing[strings.ToLower(column.Name)] {
			continue
		}
		statement := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table.qualifiedName(), columnDeclaration(column))
		if _, err := table.transaction.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (table tableTarget) existingColumns() (map[string]bool, error) {
	rows, err := table.transaction.Query(`SELECT name FROM pragma_table_info(?, ?)`, table.name, table.schema())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		existing[strings.ToLower(name)] = true
	}
	return existing, rows.Err()
}

func (table tableTarget) create(columns []columnDefinition) error {
	declarations := make([]string, len(columns))
	for index, column := range columns {
		declarations[index] = columnDeclaration(column)
	}
	statement := fmt.Sprintf("CREATE TABLE %s (%s)", table.qualifiedName(), strings.Join(declarations, ", "))
	_, err := table.transaction.Exec(statement)
	return err
}

func (table tableTarget) prepareInsert(names []string) (*sql.Stmt, error) {
	quotedNames := make([]string, len(names))
	placeholders := make([]string, len(names))
	for index, name := range names {
		quotedNames[index] = quoteIdentifier(name)
		placeholders[index] = "?"
	}
	statement := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table.qualifiedName(), strings.Join(quotedNames, ", "), strings.Join(placeholders, ", "))
	return table.transaction.Prepare(statement)
}

func columnDeclaration(column columnDefinition) string {
	return strings.TrimSpace(quoteIdentifier(column.Name) + " " + column.SQLType)
}
