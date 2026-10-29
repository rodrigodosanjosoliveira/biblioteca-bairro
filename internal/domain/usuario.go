package domain

import (
	"errors"
	"strings"
)

// ErrNomeVazio indica que o nome do usuário está vazio depois de aparado.
var ErrNomeVazio = errors.New("usuario: nome vazio")

// Usuario é um leitor da biblioteca. ID 0 significa ainda não persistido.
type Usuario struct {
	ID   int64
	Nome string
}

// NovoUsuario cria um Usuario com o nome aparado (strings.TrimSpace). Nome
// vazio depois de aparado devolve ErrNomeVazio e Usuario{}.
func NovoUsuario(nome string) (Usuario, error) {
	n := strings.TrimSpace(nome)
	if n == "" {
		return Usuario{}, ErrNomeVazio
	}
	return Usuario{Nome: n}, nil
}
