// Package migrations embute as migrações SQL versionadas do esquema.
package migrations

import "embed"

// FS contém os arquivos NNNN_nome.up.sql e NNNN_nome.down.sql.
//
//go:embed *.sql
var FS embed.FS
