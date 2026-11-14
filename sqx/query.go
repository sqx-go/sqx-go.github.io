// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const (
	defaultStdinTableName = "stdin"
	stdinPreviewRowLimit  = 100
	usageOverview         = `sqx — SQLite eXtended for Unix streams and web intelligence

Usage:
  cat data.csv | sqx [flags] "<SQL>"
  sqx [flags] -f users=users.csv "<SQL>"
  cat urls.txt | sqx prefetch [-c 20]
  sqx import -db project.db -t mytable [file_or_url ...]
  sqx cache [stats | ls | query "<SQL>" | clear -all | clear -older-than 24h]
  sqx tables -db project.db
  sqx schema -db project.db
  sqx version

Flags may appear before or after the SQL text.

Flags:
`
)

var errMissingQuery = errors.New("no SQL query given (pipe data in or pass a query; see sqx -h)")

type QueryOptions struct {
	DatabasePath     string
	CachePath        string
	StdinTableName   string
	StdinFormat      string
	OutputFormat     string
	SaveTableName    string
	InputHasNoHeader bool
	OmitOutputHeader bool
	ShowVersion      bool
	FileSources      repeatableFlag
	Fetch            FetchSettings
}

type repeatableFlag []string

func (values *repeatableFlag) String() string { return strings.Join(*values, ",") }

func (values *repeatableFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runQueryCommand(arguments []string) error {
	options := QueryOptions{Fetch: defaultFetchSettings()}
	flags := newQueryFlagSet(&options)
	positional, err := parseFlagsAnywhere(flags, arguments)
	if err != nil {
		return err
	}
	if options.ShowVersion {
		return runVersionCommand(nil)
	}
	if err := validateOutputFormat(options.OutputFormat); err != nil {
		return err
	}
	query := strings.TrimSpace(strings.Join(positional, " "))
	loadsStdin := shouldLoadStdin(options.StdinTableName, query)
	if query == "" && !loadsStdin {
		flags.Usage()
		return errMissingQuery
	}

	fetcher, err := openWebFetcher(options.CachePath, options.Fetch)
	if err != nil {
		return err
	}
	defer fetcher.Close()
	useWebFetcher(fetcher)

	database, err := openDatabase(options.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := loadFileSources(database, fetcher, options); err != nil {
		return err
	}
	if loadsStdin {
		if err := loadStdin(database, options); err != nil {
			return err
		}
		if query == "" {
			query = fmt.Sprintf("SELECT * FROM %s LIMIT %d", quoteIdentifier(options.StdinTableName), stdinPreviewRowLimit)
		}
	}
	return executeQuery(database, query, options)
}

func newQueryFlagSet(options *QueryOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("sqx", flag.ContinueOnError)
	flags.StringVar(&options.DatabasePath, "db", inMemoryDatabasePath, "SQLite database path (:memory: or a file path)")
	flags.StringVar(&options.StdinTableName, "t", defaultStdinTableName, "Table name for piped stdin data")
	flags.StringVar(&options.StdinFormat, "i", formatAuto, "Input format for stdin (auto, csv, tsv, jsonl, json, lines)")
	flags.StringVar(&options.OutputFormat, "o", outputAuto, "Output format (auto, csv, tsv, table, md, json, jsonl, raw)")
	flags.StringVar(&options.SaveTableName, "save", "", "Save the query result into this table in -db, replacing it if it exists")
	flags.BoolVar(&options.InputHasNoHeader, "no-header", false, "CSV/TSV input has no header row (columns become c1, c2, ...)")
	flags.BoolVar(&options.OmitOutputHeader, "no-output-header", false, "Omit the header row in csv/tsv output")
	flags.BoolVar(&options.ShowVersion, "version", false, "Print the version and exit")
	flags.Var(&options.FileSources, "f", "Load a file or URL as a table: -f name=path.csv or -f path.csv (repeatable)")
	registerFetchFlags(flags, &options.Fetch, &options.CachePath)
	flags.Usage = func() {
		fmt.Fprint(flags.Output(), usageOverview)
		flags.PrintDefaults()
	}
	return flags
}

func registerFetchFlags(flags *flag.FlagSet, settings *FetchSettings, cachePath *string) {
	flags.StringVar(cachePath, "cache-db", defaultCachePath(), "Persistent HTTP/DNS cache SQLite file")
	flags.BoolVar(&settings.Offline, "offline", settings.Offline, "Never make network requests (use the cache only)")
	flags.BoolVar(&settings.ForceRefresh, "force", settings.ForceRefresh, "Ignore cached responses and fetch again")
	flags.DurationVar(&settings.CacheTTL, "cache-ttl", settings.CacheTTL, "Max age of cached responses (0s always refetches)")
	flags.DurationVar(&settings.RequestTimeout, "timeout", settings.RequestTimeout, "HTTP request timeout")
	flags.StringVar(&settings.UserAgent, "ua", settings.UserAgent, "HTTP User-Agent header")
	flags.Int64Var(&settings.MaxBodyBytes, "max-body-bytes", settings.MaxBodyBytes, "Max response body size to keep per URL")
}

func parseFlagsAnywhere(flags *flag.FlagSet, arguments []string) ([]string, error) {
	var positional []string
	for {
		if err := flags.Parse(arguments); err != nil {
			return nil, err
		}
		remaining := flags.Args()
		if len(remaining) == 0 {
			return positional, nil
		}
		if isAfterTerminator(arguments, remaining) {
			return append(positional, remaining...), nil
		}
		positional = append(positional, remaining[0])
		arguments = remaining[1:]
	}
}

func isAfterTerminator(arguments, remaining []string) bool {
	consumed := len(arguments) - len(remaining)
	return consumed > 0 && arguments[consumed-1] == "--"
}

func isStdinPiped() bool {
	return !isTerminal(os.Stdin)
}

func shouldLoadStdin(tableName, query string) bool {
	if !isStdinPiped() {
		return false
	}
	return query == "" || mentionsIdentifier(query, tableName)
}

func mentionsIdentifier(query, identifier string) bool {
	pattern := `(?i)(^|[^A-Za-z0-9_])["'\x60\[]?` + regexp.QuoteMeta(identifier) + `["'\x60\]]?([^A-Za-z0-9_]|$)`
	return regexp.MustCompile(pattern).MatchString(query)
}

func loadStdin(database *sql.DB, options QueryOptions) error {
	err := loadTable(database, TableSource{
		Reader:    os.Stdin,
		TableName: options.StdinTableName,
		Format:    options.StdinFormat,
		HasHeader: !options.InputHasNoHeader,
		Temporary: true,
	})
	if err != nil {
		return fmt.Errorf("loading stdin: %w", err)
	}
	return nil
}

func loadFileSources(database *sql.DB, fetcher *WebFetcher, options QueryOptions) error {
	for _, specification := range options.FileSources {
		tableName, location := parseFileSource(specification)
		if err := loadFileSource(database, fetcher, tableName, location, !options.InputHasNoHeader); err != nil {
			return err
		}
	}
	return nil
}

func parseFileSource(specification string) (tableName, location string) {
	name, path, hasName := strings.Cut(specification, "=")
	if hasName && isPlainIdentifier(name) {
		return sanitizeIdentifier(name, 0), path
	}
	if hasHTTPScheme(specification) {
		return tableNameFromPath(urlPath(specification)), specification
	}
	return tableNameFromPath(specification), specification
}

func loadFileSource(database *sql.DB, fetcher *WebFetcher, tableName, location string, hasHeader bool) error {
	reader, format, err := openSource(fetcher, location)
	if err != nil {
		return fmt.Errorf("opening %s: %w", location, err)
	}
	defer reader.Close()
	return loadTable(database, TableSource{
		Reader:    reader,
		TableName: tableName,
		Format:    format,
		HasHeader: hasHeader,
		Temporary: true,
	})
}

func openSource(fetcher *WebFetcher, location string) (io.ReadCloser, string, error) {
	if hasHTTPScheme(location) {
		response := fetcher.Fetch(location)
		if response.Error != "" {
			return nil, "", errors.New(response.Error)
		}
		if response.StatusCode >= 400 {
			return nil, "", fmt.Errorf("HTTP status %d", response.StatusCode)
		}
		return io.NopCloser(strings.NewReader(response.Body)), formatFromExtension(urlPath(response.FinalURL)), nil
	}
	file, err := os.Open(location)
	if err != nil {
		return nil, "", err
	}
	return file, formatFromExtension(location), nil
}

func urlPath(rawURL string) string {
	parsed, ok := parseURLAssumingHTTPS(rawURL)
	if !ok {
		return rawURL
	}
	return parsed.Path
}

func executeQuery(database *sql.DB, query string, options QueryOptions) error {
	if options.SaveTableName != "" {
		if err := saveQueryResult(database, options.SaveTableName, query); err != nil {
			return err
		}
		query = "SELECT * FROM " + mainTable(options.SaveTableName)
	}
	if !returnsRows(query) {
		return executeStatement(database, query)
	}
	rows, err := database.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	source, err := newSQLRowSource(rows)
	if err != nil {
		return err
	}
	return renderRows(os.Stdout, source, RenderOptions{Format: options.OutputFormat, OmitHeader: options.OmitOutputHeader})
}

func executeStatement(database *sql.DB, statement string) error {
	result, err := database.Exec(statement)
	if err != nil {
		return err
	}
	if isTerminal(os.Stderr) {
		affected, _ := result.RowsAffected()
		fmt.Fprintf(os.Stderr, "OK (%d rows affected)\n", affected)
	}
	return nil
}
