package httpapi

import (
	"fmt"
	"testing"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// DEL-014: usuario_inexistente (404) entra no catálogo para GET
// /usuarios/{id}; usuario_nao_encontrado continua 422 e classificar continua
// levando storage.ErrUsuarioNaoEncontrado a ele (o 404 é decidido só no
// handler do detalhe).
func TestCatalogoUsuarioInexistente(t *testing.T) {
	catalogoTests := []struct {
		codigo   string
		status   int
		mensagem string
	}{
		{codigo: "usuario_inexistente", status: 404, mensagem: "usuário não encontrado"},
		{codigo: "usuario_nao_encontrado", status: 422, mensagem: "usuário não encontrado"},
	}
	for _, tt := range catalogoTests {
		t.Run("catalogo/"+tt.codigo, func(t *testing.T) {
			e, ok := catalogo[tt.codigo]
			if !ok {
				t.Fatalf("código %q fora do catálogo", tt.codigo)
			}
			if e.status != tt.status || e.mensagem != tt.mensagem {
				t.Errorf("catálogo[%q] = %d %q, want %d %q", tt.codigo, e.status, e.mensagem, tt.status, tt.mensagem)
			}
		})
	}

	classificarTests := []struct {
		name string
		err  error
	}{
		{name: "ErrUsuarioNaoEncontrado", err: storage.ErrUsuarioNaoEncontrado},
		{name: "ErrUsuarioNaoEncontrado embrulhado", err: fmt.Errorf("x: %w", storage.ErrUsuarioNaoEncontrado)},
	}
	for _, tt := range classificarTests {
		t.Run("classificar/"+tt.name, func(t *testing.T) {
			if cod, _ := classificar(tt.err); cod != "usuario_nao_encontrado" {
				t.Errorf("classificar = %q, want usuario_nao_encontrado (o 404 é só do detalhe)", cod)
			}
		})
	}
}
