// Package migrations embute as migrações SQL versionadas do esquema.
package migrations

import "embed"

// FS contém os arquivos NNNN_nome.up.sql e NNNN_nome.down.sql.
//
//go:embed *.sql
var FS embed.FS

// Controle contém a migração 0000 da tabela de controle do executor
// (controle/0000_migracao_aplicada.{up,down}.sql). Fica fora de FS: o executor
// a aplica por conta própria e FS só traz as migrações do esquema.
//
//go:embed controle/*.sql
var Controle embed.FS
