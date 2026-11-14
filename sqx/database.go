// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	databaseBusyTimeoutMS = 10000
	stagingTableSuffix    = "_sqx_staging"
)

var (
	leadingSQLComments   = regexp.MustCompile(`^(\s*(--[^\n]*(\n|$)|/\*(.|\n)*?\*/))*\s*`)
	returningClause      = regexp.MustCompile(`(?i)\bRETURNING\b`)
	rowReturningKeywords = []string{"SELECT", "WITH", "PRAGMA", "EXPLAIN", "VALUES"}
)

func sqliteDSN(path string, pragmas ...string) string {
	if len(pragmas) == 0 {
		return path
	}
	parameters := make([]string, len(pragmas))
	for index, pragma := range pragmas {
		parameters[index] = "_pragma=" + pragma
	}
	return path + "?" + strings.Join(parameters, "&")
}

func openDatabase(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", sqliteDSN(path, fmt.Sprintf("busy_timeout(%d)", databaseBusyTimeoutMS)))
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	return database, nil
}

func openExistingDatabase(path string) (*sql.DB, error) {
	if path != inMemoryDatabasePath {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("database %s: %w", path, err)
		}
	}
	return openDatabase(path)
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func mainTable(name string) string {
	return "main." + quoteIdentifier(name)
}

func returnsRows(query string) bool {
	statement := strings.ToUpper(leadingSQLComments.ReplaceAllString(query, ""))
	for _, keyword := range rowReturningKeywords {
		if strings.HasPrefix(statement, keyword) {
			return true
		}
	}
	return returningClause.MatchString(statement)
}

func saveQueryResult(database *sql.DB, tableName, query string) error {
	if tableName == "" {
		return errors.New("save table name is empty")
	}
	stagingTable := mainTable(tableName + stagingTableSuffix)
	transaction, err := database.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()

	statements := []string{
		"DROP TABLE IF EXISTS " + stagingTable,
		fmt.Sprintf("CREATE TABLE %s AS %s", stagingTable, query),
		"DROP TABLE IF EXISTS " + mainTable(tableName),
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s", stagingTable, quoteIdentifier(tableName)),
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return fmt.Errorf("saving query result to table %s: %w", tableName, err)
		}
	}
	return transaction.Commit()
}

func countRows(database *sql.DB, qualifiedTable string) (int64, error) {
	var count int64
	err := database.QueryRow("SELECT COUNT(*) FROM " + qualifiedTable).Scan(&count)
	return count, err
}
