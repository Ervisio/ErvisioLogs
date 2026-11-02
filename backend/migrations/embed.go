// Package migrations embeds the SQL migration files shipped with the binary.
package migrations

import "embed"

//go:embed postgres/*.sql clickhouse/*.sql
var FS embed.FS

const (
	PostgresDir   = "postgres"
	ClickHouseDir = "clickhouse"
)
