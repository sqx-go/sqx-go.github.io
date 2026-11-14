// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"bufio"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultPrefetchWorkers = 10
	defaultWorkspacePath   = "sqx.db"
	defaultImportTableName = "data"
	cacheStatsQuery        = `SELECT
		COUNT(*) AS cached_urls,
		COALESCE(SUM(status_code BETWEEN 200 AND 299), 0) AS status_2xx,
		COALESCE(SUM(status_code >= 400 OR status_code = 0), 0) AS errors_or_4xx_5xx,
		ROUND(AVG(response_ms), 1) AS avg_ms,
		ROUND(COALESCE(SUM(LENGTH(body)), 0) / 1024.0 / 1024.0, 2) AS total_body_mb,
		(SELECT COUNT(*) FROM dns_cache) AS cached_dns_lookups
	  FROM http_cache`
	cacheListQuery = `SELECT url, status_code, response_ms, LENGTH(body) AS bytes,
		datetime(fetched_at, 'unixepoch') AS fetched_utc
	  FROM http_cache ORDER BY fetched_at DESC`
)

var prefetchColumns = []string{"url", "final_url", "status_code", "response_ms", "size_bytes", "title", "error"}

func runPrefetchCommand(arguments []string) error {
	settings := defaultFetchSettings()
	var cachePath string
	flags := flag.NewFlagSet("prefetch", flag.ContinueOnError)
	workerCount := flags.Int("c", defaultPrefetchWorkers, "Number of concurrent HTTP workers")
	outputFormat := flags.String("o", outputAuto, "Output format (auto, csv, tsv, table, md, json, jsonl, raw)")
	registerFetchFlags(flags, &settings, &cachePath)
	positional, err := parseFlagsAnywhere(flags, arguments)
	if err != nil {
		return err
	}
	if err := validateOutputFormat(*outputFormat); err != nil {
		return err
	}
	urls, err := collectPrefetchURLs(positional)
	if err != nil {
		return err
	}

	fetcher, err := openWebFetcher(cachePath, settings)
	if err != nil {
		return err
	}
	defer fetcher.Close()

	responses := fetchConcurrently(fetcher, urls, max(*workerCount, 1))
	source := newStaticRowSource(prefetchColumns, prefetchRecords(responses))
	return renderRows(os.Stdout, source, RenderOptions{Format: *outputFormat})
}

func collectPrefetchURLs(arguments []string) ([]string, error) {
	urls := uniqueNonEmpty(arguments)
	if len(urls) == 0 && isStdinPiped() {
		lines, err := readPrefetchLines(os.Stdin)
		if err != nil {
			return nil, err
		}
		urls = uniqueNonEmpty(lines)
	}
	if len(urls) == 0 {
		return nil, errors.New("no URLs to prefetch (pipe URLs via stdin or pass them as arguments)")
	}
	return urls, nil
}

func readPrefetchLines(file *os.File) ([]string, error) {
	var urls []string
	scanner := newLineScanner(bufio.NewReader(file))
	for scanner.Scan() {
		if target, isURL := prefetchTarget(scanner.Text()); isURL {
			urls = append(urls, target)
		}
	}
	return urls, scanner.Err()
}

func prefetchTarget(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.EqualFold(line, "url") || strings.EqualFold(line, "line") {
		return "", false
	}
	if hasHTTPScheme(line) {
		firstField, _, _ := strings.Cut(line, ",")
		return firstField, true
	}
	return line, true
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]bool{}
	var unique []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	return unique
}

func fetchConcurrently(fetcher *WebFetcher, urls []string, workerCount int) []*HTTPResponse {
	responses := make([]*HTTPResponse, len(urls))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(workerCount, len(urls)) {
		workers.Go(func() {
			for index := range jobs {
				responses[index] = fetcher.Fetch(urls[index])
			}
		})
	}
	for index := range urls {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return responses
}

func prefetchRecords(responses []*HTTPResponse) [][]any {
	records := make([][]any, len(responses))
	for index, response := range responses {
		records[index] = []any{
			response.URL, response.FinalURL, int64(response.StatusCode), response.ResponseTimeMS,
			int64(len(response.Body)), pageTitle(response.Body), response.Error,
		}
	}
	return records
}

func pageTitle(body string) string {
	if body == "" {
		return ""
	}
	titles := extractTagTexts(body, "title")
	if len(titles) == 0 {
		return ""
	}
	return titles[0]
}

type importOptions struct {
	databasePath     string
	cachePath        string
	tableName        string
	inputFormat      string
	inputHasNoHeader bool
	replaceTable     bool
	fetch            FetchSettings
	sources          []string
}

func runImportCommand(arguments []string) error {
	options, err := parseImportFlags(arguments)
	if err != nil {
		return err
	}
	fetcher, err := openWebFetcher(options.cachePath, options.fetch)
	if err != nil {
		return err
	}
	defer fetcher.Close()

	database, err := openDatabase(options.databasePath)
	if err != nil {
		return err
	}
	defer database.Close()

	if options.replaceTable {
		if _, err := database.Exec("DROP TABLE IF EXISTS " + mainTable(options.tableName)); err != nil {
			return err
		}
	}
	request := importRequest{
		database:  database,
		fetcher:   fetcher,
		tableName: options.tableName,
		format:    options.inputFormat,
		hasHeader: !options.inputHasNoHeader,
	}
	if err := request.importAll(options.sources); err != nil {
		return err
	}
	return reportImportedRows(database, options)
}

func parseImportFlags(arguments []string) (importOptions, error) {
	options := importOptions{fetch: defaultFetchSettings()}
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.StringVar(&options.databasePath, "db", defaultWorkspacePath, "Target SQLite database file")
	flags.StringVar(&options.tableName, "t", defaultImportTableName, "Target table name")
	flags.StringVar(&options.inputFormat, "i", formatAuto, "Input format (auto, csv, tsv, jsonl, json, lines)")
	flags.BoolVar(&options.inputHasNoHeader, "no-header", false, "CSV/TSV input has no header row")
	flags.BoolVar(&options.replaceTable, "replace", false, "Drop the table before importing instead of appending")
	registerFetchFlags(flags, &options.fetch, &options.cachePath)
	sources, err := parseFlagsAnywhere(flags, arguments)
	if err != nil {
		return options, err
	}
	if len(sources) == 0 && !isStdinPiped() {
		return options, errors.New("nothing to import (pass files or URLs, or pipe data via stdin)")
	}
	options.sources = sources
	return options, nil
}

func reportImportedRows(database *sql.DB, options importOptions) error {
	count, err := countRows(database, mainTable(options.tableName))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Imported into %s (table %q now has %d rows)\n", options.databasePath, options.tableName, count)
	return nil
}

type importRequest struct {
	database  *sql.DB
	fetcher   *WebFetcher
	tableName string
	format    string
	hasHeader bool
}

func (request importRequest) importAll(sources []string) error {
	if len(sources) == 0 {
		return loadTable(request.database, request.tableSource(os.Stdin, request.format))
	}
	for _, location := range sources {
		if err := request.importOne(location); err != nil {
			return err
		}
	}
	return nil
}

func (request importRequest) importOne(location string) error {
	reader, detectedFormat, err := openSource(request.fetcher, location)
	if err != nil {
		return fmt.Errorf("opening %s: %w", location, err)
	}
	defer reader.Close()
	format := request.format
	if format == formatAuto {
		format = detectedFormat
	}
	return loadTable(request.database, request.tableSource(reader, format))
}

func (request importRequest) tableSource(reader io.Reader, format string) TableSource {
	return TableSource{Reader: reader, TableName: request.tableName, Format: format, HasHeader: request.hasHeader}
}

func runCacheCommand(arguments []string) error {
	flags := flag.NewFlagSet("cache", flag.ContinueOnError)
	cachePath := flags.String("cache-db", defaultCachePath(), "Persistent HTTP/DNS cache SQLite file")
	olderThan := flags.Duration("older-than", 0, "For 'clear': remove entries older than this duration (e.g. 24h)")
	clearAll := flags.Bool("all", false, "For 'clear': remove every cached HTTP and DNS entry")
	outputFormat := flags.String("o", outputAuto, "Output format")
	positional, err := parseFlagsAnywhere(flags, arguments)
	if err != nil {
		return err
	}
	action := "stats"
	if len(positional) > 0 {
		action = strings.ToLower(positional[0])
	}

	store, err := openCacheStore(*cachePath)
	if err != nil {
		return err
	}
	defer store.Close()

	render := RenderOptions{Format: *outputFormat}
	switch action {
	case "stats":
		return renderQuery(store, cacheStatsQuery, render)
	case "ls", "list":
		return renderQuery(store, cacheListQuery, render)
	case "query":
		if len(positional) < 2 {
			return errors.New(`usage: sqx cache query "<SQL on http_cache or dns_cache>"`)
		}
		return renderQuery(store, strings.Join(positional[1:], " "), render)
	case "clear":
		return clearCache(store, *clearAll, *olderThan)
	}
	return fmt.Errorf("unknown cache action %q (expected stats, ls, query or clear)", action)
}

func renderQuery(database *sql.DB, query string, options RenderOptions) error {
	rows, err := database.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	source, err := newSQLRowSource(rows)
	if err != nil {
		return err
	}
	return renderRows(os.Stdout, source, options)
}

func clearCache(store *sql.DB, clearAll bool, olderThan time.Duration) error {
	switch {
	case clearAll:
		return clearEntireCache(store)
	case olderThan > 0:
		return clearCacheOlderThan(store, olderThan)
	}
	return errors.New("refusing to clear the cache without -all or -older-than <duration>")
}

func clearEntireCache(store *sql.DB) error {
	for _, statement := range []string{"DELETE FROM http_cache", "DELETE FROM dns_cache", "VACUUM"} {
		if _, err := store.Exec(statement); err != nil {
			return err
		}
	}
	fmt.Fprintln(os.Stderr, "Cleared all cached HTTP and DNS entries.")
	return nil
}

func clearCacheOlderThan(store *sql.DB, olderThan time.Duration) error {
	cutoffUnix := time.Now().Add(-olderThan).Unix()
	var removed int64
	for _, table := range []string{"http_cache", "dns_cache"} {
		result, err := store.Exec("DELETE FROM "+table+" WHERE fetched_at < ?", cutoffUnix)
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		removed += affected
	}
	fmt.Fprintf(os.Stderr, "Removed %d cache entries older than %s.\n", removed, olderThan)
	return nil
}

type inspectionOptions struct {
	databasePath string
	render       RenderOptions
}

func parseInspectionFlags(name string, arguments []string) (inspectionOptions, error) {
	var options inspectionOptions
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.StringVar(&options.databasePath, "db", defaultWorkspacePath, "SQLite database file to inspect")
	flags.StringVar(&options.render.Format, "o", outputAuto, "Output format")
	_, err := parseFlagsAnywhere(flags, arguments)
	return options, err
}

func runSchemaCommand(arguments []string) error {
	options, err := parseInspectionFlags("schema", arguments)
	if err != nil {
		return err
	}
	database, err := openExistingDatabase(options.databasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	return renderQuery(database, `SELECT name AS table_name, sql AS schema_sql FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`, options.render)
}

func runTablesCommand(arguments []string) error {
	options, err := parseInspectionFlags("tables", arguments)
	if err != nil {
		return err
	}
	database, err := openExistingDatabase(options.databasePath)
	if err != nil {
		return err
	}
	defer database.Close()

	names, err := listTableNames(database)
	if err != nil {
		return err
	}
	records := make([][]any, len(names))
	for index, name := range names {
		count, err := countRows(database, mainTable(name))
		if err != nil {
			return err
		}
		records[index] = []any{name, count}
	}
	source := newStaticRowSource([]string{"table_name", "row_count"}, records)
	return renderRows(os.Stdout, source, options.render)
}

func listTableNames(database *sql.DB) ([]string, error) {
	rows, err := database.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
