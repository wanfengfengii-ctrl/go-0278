// Package migrations embeds the relational schema so the service can apply it
// to any SQL database at startup without an external migration runner.
package migrations

import _ "embed"

// InitSQL is the full initial schema, applied idempotently on open.
//
//go:embed 001_init.sql
var InitSQL string
