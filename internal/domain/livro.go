package domain

import (
	"errors"
	"strings"
)

// ErrTituloVazio indica que o título do livro está vazio depois de aparado.
var ErrTituloVazio = errors.New("livro: título vazio")

// Livro é um exemplar do acervo. ID 0 significa ainda não persistido.
type Livro struct {
	ID     int64
	Titulo string
}

// NovoLivro cria um Livro com o título aparado (strings.TrimSpace). Título
// vazio depois de aparado devolve ErrTituloVazio e Livro{}.
func NovoLivro(titulo string) (Livro, error) {
	t := strings.TrimSpace(titulo)
	if t == "" {
		return Livro{}, ErrTituloVazio
	}
	return Livro{Titulo: t}, nil
}
