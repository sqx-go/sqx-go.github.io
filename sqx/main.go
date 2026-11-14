// SPDX-License-Identifier: GPL-2.0-or-later
// Copyright (C) 2026 Abhishek Sengar

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

var buildVersion = "dev"

func main() {
	err := run(os.Args[1:])
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return
	}
	fmt.Fprintf(os.Stderr, "sqx: %v\n", err)
	os.Exit(1)
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return runQueryCommand(arguments)
	}
	command, isSubcommand := findSubcommand(arguments[0])
	if !isSubcommand {
		return runQueryCommand(arguments)
	}
	return command(arguments[1:])
}

func findSubcommand(name string) (func(arguments []string) error, bool) {
	switch name {
	case "query":
		return runQueryCommand, true
	case "prefetch", "crawl":
		return runPrefetchCommand, true
	case "import":
		return runImportCommand, true
	case "cache":
		return runCacheCommand, true
	case "tables":
		return runTablesCommand, true
	case "schema":
		return runSchemaCommand, true
	case "version":
		return runVersionCommand, true
	}
	return nil, false
}

func runVersionCommand(arguments []string) error {
	fmt.Println("sqx", buildVersion)
	return nil
}
