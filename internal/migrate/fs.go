package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

// Migracao descreve uma migração do conjunto: o número, o nome e o checksum
// (SHA-256 em hexadecimal minúsculo) dos bytes do .up.sql.
type Migracao struct {
	Versao   int
	Nome     string
	Checksum string
}

// arquivoMigracao é uma migração com o que o executor precisa para aplicá-la.
type arquivoMigracao struct {
	Migracao
	arquivo string // nome do .up.sql
	sql     string // conteúdo do .up.sql
}

// reArquivo reconhece NNNN_nome.up.sql e NNNN_nome.down.sql: quatro dígitos
// ASCII, nome de [a-z0-9_]+ e direção em minúsculas.
var reArquivo = regexp.MustCompile(`^([0-9]{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// ValidarFS confere o conjunto de migrações de fsys e devolve as migrações em
// ordem de versão. É pura: só lê fsys. O conjunto é válido quando a raiz tem
// somente pares NNNN_nome.up.sql / NNNN_nome.down.sql, com versões 1..N sem
// lacuna nem repetição e o mesmo nome nos dois arquivos de cada par. Sem
// nenhuma migração também é inválido. Em erro devolve nil e um ErroFSInvalido
// que cita o arquivo (ou o seu par) quando há.
func ValidarFS(fsys fs.FS) ([]Migracao, error) {
	ms, err := carregar(fsys)
	if err != nil {
		return nil, err
	}
	out := make([]Migracao, len(ms))
	for i, m := range ms {
		out[i] = m.Migracao
	}
	return out, nil
}

// par agrega o que a raiz tem de uma versão.
type par struct {
	nome     string
	up, down bool
}

// carregar valida fsys e lê o .up.sql de cada migração.
func carregar(fsys fs.FS) ([]arquivoMigracao, error) {
	entradas, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, invalido("")
	}
	pares := map[int]*par{}
	for _, ent := range entradas {
		nome := ent.Name()
		if !ent.Type().IsRegular() {
			return nil, invalido(nome)
		}
		m := reArquivo.FindStringSubmatch(nome)
		if m == nil {
			return nil, invalido(nome)
		}
		versao, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, invalido(nome)
		}
		p := pares[versao]
		if p == nil {
			p = &par{nome: m[2]}
			pares[versao] = p
		}
		if p.nome != m[2] {
			return nil, invalido(nome)
		}
		if m[3] == "up" {
			p.up = true
		} else {
			p.down = true
		}
	}
	if len(pares) == 0 {
		return nil, invalido("")
	}

	versoes := make([]int, 0, len(pares))
	for v := range pares {
		versoes = append(versoes, v)
	}
	sort.Ints(versoes)

	out := make([]arquivoMigracao, 0, len(versoes))
	for i, v := range versoes {
		p := pares[v]
		up, down := nomeArquivo(v, p.nome, "up"), nomeArquivo(v, p.nome, "down")
		switch {
		case !p.up:
			return nil, invalido(down)
		case !p.down:
			return nil, invalido(up)
		case v != i+1:
			// As versões têm de ser 1..N, em sequência.
			return nil, invalido(up)
		}
		conteudo, err := fs.ReadFile(fsys, up)
		if err != nil {
			return nil, invalido(up)
		}
		soma := sha256.Sum256(conteudo)
		out = append(out, arquivoMigracao{
			Migracao: Migracao{Versao: v, Nome: p.nome, Checksum: hex.EncodeToString(soma[:])},
			arquivo:  up,
			sql:      string(conteudo),
		})
	}
	return out, nil
}

func nomeArquivo(versao int, nome, direcao string) string {
	return strconv.Itoa(10000 + versao)[1:] + "_" + nome + "." + direcao + ".sql"
}

func invalido(arquivo string) Erro {
	e := ErroFSInvalido
	e.Arquivo = arquivo
	return e
}

// TemConcurrently diz se sql contém a palavra CONCURRENTLY, sem distinguir
// maiúsculas de minúsculas, em qualquer ponto: código, comentário ou literal.
// A palavra é delimitada por início ou fim do texto ou por um byte que não é
// letra ASCII, dígito nem sublinhado (bytes não ASCII contam como limite). A
// busca é deliberadamente larga: é preferível recusar um comentário a deixar
// passar um CREATE INDEX CONCURRENTLY, que não roda em transação.
func TemConcurrently(sql string) bool {
	const palavra = "concurrently"
	for i := 0; i+len(palavra) <= len(sql); i++ {
		if i > 0 && ehPalavra(sql[i-1]) {
			continue
		}
		if !igualASCII(sql[i:i+len(palavra)], palavra) {
			continue
		}
		if fim := i + len(palavra); fim == len(sql) || !ehPalavra(sql[fim]) {
			return true
		}
	}
	return false
}

// ehPalavra diz se c é letra ASCII, dígito ou sublinhado.
func ehPalavra(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// igualASCII compara s com minusculo (só letras minúsculas ASCII) ignorando a
// caixa das letras ASCII de s.
func igualASCII(s, minusculo string) bool {
	if len(s) != len(minusculo) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != minusculo[i] {
			return false
		}
	}
	return true
}
