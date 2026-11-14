// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"bufio"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	outputAuto     = "auto"
	outputCSV      = "csv"
	outputTSV      = "tsv"
	outputRaw      = "raw"
	outputJSONL    = "jsonl"
	outputJSON     = "json"
	outputMarkdown = "md"
	outputTable    = "table"

	maxTableCellRunes = 80
	truncationMarker  = "..."
)

var outputAliases = map[string]string{
	"auto": outputAuto, "csv": outputCSV, "tsv": outputTSV,
	"raw": outputRaw, "lines": outputRaw,
	"jsonl": outputJSONL, "ndjson": outputJSONL, "json": outputJSON,
	"md": outputMarkdown, "markdown": outputMarkdown,
	"table": outputTable, "ascii": outputTable,
}

type RowSource interface {
	Columns() []string
	Next() bool
	Values() ([]any, error)
	Err() error
}

type RenderOptions struct {
	Format     string
	OmitHeader bool
}

func renderRows(output io.Writer, source RowSource, options RenderOptions) error {
	format, err := resolveOutputFormat(options.Format)
	if err != nil {
		return err
	}
	switch format {
	case outputCSV:
		err = renderDelimited(output, source, ',', options.OmitHeader)
	case outputTSV:
		err = renderDelimited(output, source, '\t', options.OmitHeader)
	case outputRaw:
		err = renderRawLines(output, source)
	case outputJSONL:
		err = renderJSONLines(output, source)
	case outputJSON:
		err = renderJSONArray(output, source)
	case outputMarkdown:
		err = renderMarkdownTable(output, source)
	case outputTable:
		err = renderASCIITable(output, source)
	}
	if err != nil {
		return err
	}
	return source.Err()
}

func validateOutputFormat(format string) error {
	_, err := resolveOutputFormat(format)
	return err
}

func resolveOutputFormat(requested string) (string, error) {
	format, known := outputAliases[strings.ToLower(strings.TrimSpace(requested))]
	if requested == "" {
		format, known = outputAuto, true
	}
	if !known {
		return "", fmt.Errorf("unknown output format %q (expected auto, csv, tsv, table, md, json, jsonl or raw)", requested)
	}
	if format != outputAuto {
		return format, nil
	}
	if isTerminal(os.Stdout) {
		return outputTable, nil
	}
	return outputCSV, nil
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

type sqlRowSource struct {
	rows    *sql.Rows
	columns []string
}

func newSQLRowSource(rows *sql.Rows) (*sqlRowSource, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	return &sqlRowSource{rows: rows, columns: columns}, nil
}

func (source *sqlRowSource) Columns() []string { return source.columns }
func (source *sqlRowSource) Next() bool        { return source.rows.Next() }
func (source *sqlRowSource) Err() error        { return source.rows.Err() }

func (source *sqlRowSource) Values() ([]any, error) {
	values := make([]any, len(source.columns))
	targets := make([]any, len(source.columns))
	for index := range values {
		targets[index] = &values[index]
	}
	err := source.rows.Scan(targets...)
	return values, err
}

type staticRowSource struct {
	columns  []string
	records  [][]any
	position int
}

func newStaticRowSource(columns []string, records [][]any) *staticRowSource {
	return &staticRowSource{columns: columns, records: records, position: -1}
}

func (source *staticRowSource) Columns() []string { return source.columns }
func (source *staticRowSource) Err() error        { return nil }

func (source *staticRowSource) Next() bool {
	source.position++
	return source.position < len(source.records)
}

func (source *staticRowSource) Values() ([]any, error) {
	return source.records[source.position], nil
}

func displayText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case []byte:
		return string(typed)
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

func displayTexts(values []any) []string {
	texts := make([]string, len(values))
	for index, value := range values {
		texts[index] = displayText(value)
	}
	return texts
}

func jsonValue(value any) any {
	if bytesValue, isBytes := value.([]byte); isBytes {
		return string(bytesValue)
	}
	return value
}

func renderDelimited(output io.Writer, source RowSource, separator rune, omitHeader bool) error {
	writer := csv.NewWriter(output)
	writer.Comma = separator
	if !omitHeader {
		if err := writer.Write(source.Columns()); err != nil {
			return err
		}
	}
	for source.Next() {
		values, err := source.Values()
		if err != nil {
			return err
		}
		if err := writer.Write(displayTexts(values)); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func renderRawLines(output io.Writer, source RowSource) error {
	writer := bufio.NewWriter(output)
	for source.Next() {
		values, err := source.Values()
		if err != nil {
			return err
		}
		writer.WriteString(strings.Join(displayTexts(values), "\t"))
		writer.WriteByte('\n')
	}
	return writer.Flush()
}

func rowObject(columns []string, values []any) map[string]any {
	object := make(map[string]any, len(columns))
	for index, column := range columns {
		object[column] = jsonValue(values[index])
	}
	return object
}

func renderJSONLines(output io.Writer, source RowSource) error {
	writer := bufio.NewWriter(output)
	encoder := json.NewEncoder(writer)
	for source.Next() {
		values, err := source.Values()
		if err != nil {
			return err
		}
		if err := encoder.Encode(rowObject(source.Columns(), values)); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func renderJSONArray(output io.Writer, source RowSource) error {
	objects := []map[string]any{}
	for source.Next() {
		values, err := source.Values()
		if err != nil {
			return err
		}
		objects = append(objects, rowObject(source.Columns(), values))
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(objects)
}

func renderMarkdownTable(output io.Writer, source RowSource) error {
	writer := bufio.NewWriter(output)
	writeMarkdownRow(writer, source.Columns())
	separators := make([]string, len(source.Columns()))
	for index := range separators {
		separators[index] = "---"
	}
	writeMarkdownRow(writer, separators)
	for source.Next() {
		values, err := source.Values()
		if err != nil {
			return err
		}
		writeMarkdownRow(writer, displayTexts(values))
	}
	return writer.Flush()
}

func writeMarkdownRow(writer *bufio.Writer, cells []string) {
	escaped := make([]string, len(cells))
	for index, cell := range cells {
		escaped[index] = strings.ReplaceAll(strings.ReplaceAll(cell, "\n", " "), "|", `\|`)
	}
	fmt.Fprintf(writer, "| %s |\n", strings.Join(escaped, " | "))
}

func renderASCIITable(output io.Writer, source RowSource) error {
	columns := source.Columns()
	widths := make([]int, len(columns))
	for index, column := range columns {
		widths[index] = utf8.RuneCountInString(column)
	}
	var rows [][]string
	for source.Next() {
		values, err := source.Values()
		if err != nil {
			return err
		}
		cells := make([]string, len(values))
		for index, value := range values {
			cells[index] = truncateCell(displayText(value))
			widths[index] = max(widths[index], utf8.RuneCountInString(cells[index]))
		}
		rows = append(rows, cells)
	}

	writer := bufio.NewWriter(output)
	writeTableBorder(writer, widths)
	writeTableRow(writer, columns, widths)
	writeTableBorder(writer, widths)
	for _, cells := range rows {
		writeTableRow(writer, cells, widths)
	}
	writeTableBorder(writer, widths)
	return writer.Flush()
}

func truncateCell(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	if utf8.RuneCountInString(text) <= maxTableCellRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxTableCellRunes-len(truncationMarker)]) + truncationMarker
}

func writeTableBorder(writer *bufio.Writer, widths []int) {
	writer.WriteString("+")
	for _, width := range widths {
		writer.WriteString(strings.Repeat("-", width+2) + "+")
	}
	writer.WriteString("\n")
}

func writeTableRow(writer *bufio.Writer, cells []string, widths []int) {
	writer.WriteString("|")
	for index, cell := range cells {
		padding := widths[index] - utf8.RuneCountInString(cell)
		writer.WriteString(" " + cell + strings.Repeat(" ", padding) + " |")
	}
	writer.WriteString("\n")
}
