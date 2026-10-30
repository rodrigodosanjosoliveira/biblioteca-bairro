package migrate

import (
	"strconv"
	"strings"
)

// Erro é o único tipo de erro que o executor devolve. Cada valor é identificado
// pela mensagem fixa; Arquivo, Code e Constraint só carregam o que é seguro
// mostrar: o nome do arquivo de migração, o SQLSTATE e o nome da constraint.
//
// Nunca embrulha outro erro (sem Unwrap) e nunca traz texto do servidor
// (Message, Detail, Where, Hint), trecho de SQL, valor de dado ou DSN. O valor é
// comparável; errors.Is compara só o tipo (a mensagem fixa) e ignora Arquivo,
// Code e Constraint.
type Erro struct {
	msg string

	// Arquivo é o nome do arquivo de migração envolvido, quando há.
	Arquivo string
	// Code é o SQLSTATE devolvido pelo servidor, quando há.
	Code string
	// Constraint é o nome da constraint violada, quando há.
	Constraint string
}

// Erros do executor. Use errors.Is para testá-los; as cópias com Arquivo, Code
// e Constraint preenchidos continuam casando.
var (
	// ErrLockOcupado indica que outra execução segurou o advisory lock até o
	// fim de Opcoes.LockTimeout.
	ErrLockOcupado = Erro{msg: "migrate: advisory lock ocupado por outra execução"}
	// ErroBancoSemRegistro indica um schema com relações e sem registro de
	// migrações aplicadas. Não há baseline automático.
	ErroBancoSemRegistro = Erro{msg: "migrate: schema com objetos e sem registro de migrações (baseline não é automático)"}
	// ErroChecksumDivergente indica que o arquivo difere do que foi aplicado.
	ErroChecksumDivergente = Erro{msg: "migrate: migração já aplicada diverge do arquivo"}
	// ErroTransacaoEncerrada indica que o arquivo encerrou a transação do
	// executor (COMMIT ou ROLLBACK próprio).
	ErroTransacaoEncerrada = Erro{msg: "migrate: o arquivo encerrou a transação da migração"}
	// ErroSessaoAlterada indica que o arquivo mudou o schema corrente ou o papel
	// da sessão (SET search_path, SET ROLE...); a transação dele é desfeita.
	ErroSessaoAlterada = Erro{msg: "migrate: o arquivo alterou o schema ou o papel da sessão"}
	// ErroFSInvalido indica um conjunto de migrações fora do formato.
	ErroFSInvalido = Erro{msg: "migrate: conjunto de migrações inválido"}
	// ErroConcurrently indica uso de CONCURRENTLY, que não roda em transação.
	ErroConcurrently = Erro{msg: "migrate: CONCURRENTLY não é suportado"}
	// ErroVersaoDesconhecida indica versão registrada que não está no conjunto.
	ErroVersaoDesconhecida = Erro{msg: "migrate: versão registrada ausente do conjunto de migrações"}
	// ErroRegistroComLacuna indica registro de versões que não é 1..N seguido.
	ErroRegistroComLacuna = Erro{msg: "migrate: registro de migrações com lacuna"}
	// ErroControleInvalido indica uma migracao_aplicada de estrutura inesperada.
	ErroControleInvalido = Erro{msg: "migrate: tabela de controle com estrutura inesperada"}
	// ErroSchemaIndefinido indica current_schema() nulo (search_path sem schema).
	ErroSchemaIndefinido = Erro{msg: "migrate: schema corrente indefinido"}
	// ErroSQL indica falha do servidor ao executar SQL do arquivo; Arquivo,
	// Code e Constraint dizem onde e por quê.
	ErroSQL = Erro{msg: "migrate: falha de SQL na migração"}
	// ErroBanco indica falha de comunicação ou de consulta fora de um arquivo.
	ErroBanco = Erro{msg: "migrate: falha na comunicação com o banco"}
	// ErroCancelado indica contexto cancelado ou vencido.
	ErroCancelado = Erro{msg: "migrate: operação cancelada"}
)

// Error devolve a mensagem fixa, seguida do arquivo, do SQLSTATE e da
// constraint quando houver. Sempre numa linha só.
func (e Erro) Error() string {
	s := e.msg
	if e.Arquivo != "" {
		s += ": " + seguro(e.Arquivo)
	}
	var det []string
	if e.Code != "" {
		det = append(det, "SQLSTATE "+seguro(e.Code))
	}
	if e.Constraint != "" {
		det = append(det, "constraint "+seguro(e.Constraint))
	}
	if len(det) > 0 {
		s += " (" + strings.Join(det, ", ") + ")"
	}
	return s
}

// Is casa por tipo: dois Erro são o mesmo erro se têm a mesma mensagem fixa.
func (e Erro) Is(alvo error) bool {
	a, ok := alvo.(Erro)
	return ok && a.msg == e.msg
}

// seguro devolve s como está quando é texto simples e imprimível, e entre
// aspas escapadas quando tem quebra de linha, controle ou aspas, para que a
// mensagem seja sempre de uma linha.
func seguro(s string) string {
	if q := strconv.Quote(s); q != `"`+s+`"` {
		return q
	}
	return s
}
