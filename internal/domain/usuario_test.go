package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNovoUsuario(t *testing.T) {
	tests := []struct {
		name     string
		nome     string
		wantNome string
		wantErr  error
	}{
		{name: "nome valido", nome: "Maria Silva", wantNome: "Maria Silva"},
		{name: "espacos em volta sao aparados", nome: " \tMaria Silva\n ", wantNome: "Maria Silva"},
		{name: "espacos internos preservados", nome: "  Ana  Lima  ", wantNome: "Ana  Lima"},
		{name: "vazio", nome: "", wantErr: ErrNomeVazio},
		{name: "so espacos", nome: "    ", wantErr: ErrNomeVazio},
		{name: "so tab e newline", nome: "\t\n\r ", wantErr: ErrNomeVazio},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NovoUsuario(tt.nome)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NovoUsuario(%q) erro = %v, want %v", tt.nome, err, tt.wantErr)
				}
				if got.ID != 0 || got.Nome != "" {
					t.Errorf("NovoUsuario(%q) em erro = %+v, want Usuario{}", tt.nome, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NovoUsuario(%q) erro inesperado: %v", tt.nome, err)
			}
			if got.Nome != tt.wantNome {
				t.Errorf("NovoUsuario(%q).Nome = %q, want %q", tt.nome, got.Nome, tt.wantNome)
			}
			if got.ID != 0 {
				t.Errorf("NovoUsuario(%q).ID = %d, want 0", tt.nome, got.ID)
			}
		})
	}
}

func TestErrNomeVazioMensagem(t *testing.T) {
	if ErrNomeVazio == nil {
		t.Fatal("ErrNomeVazio nil")
	}
	if got, want := ErrNomeVazio.Error(), "usuario: nome vazio"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// FuzzNomeObrigatorio cobre NovoLivro e NovoUsuario: o nome é aceito se e
// somente se sobra algo depois do TrimSpace, e o campo guarda o valor aparado.
func FuzzNomeObrigatorio(f *testing.F) {
	for _, s := range []string{"", " ", "\t\n\r", "Dom Casmurro", "  Maria  ", " ", " x ", "\xff", "a\x00b"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		aparado := strings.TrimSpace(s)
		valido := aparado != ""

		l, err := NovoLivro(s)
		if (err == nil) != valido {
			t.Fatalf("NovoLivro(%q) erro = %v, valido esperado = %v", s, err, valido)
		}
		if err != nil {
			if !errors.Is(err, ErrTituloVazio) {
				t.Errorf("NovoLivro(%q) erro = %v, want ErrTituloVazio", s, err)
			}
		} else if l.Titulo != aparado {
			t.Errorf("NovoLivro(%q).Titulo = %q, want %q", s, l.Titulo, aparado)
		}

		u, err := NovoUsuario(s)
		if (err == nil) != valido {
			t.Fatalf("NovoUsuario(%q) erro = %v, valido esperado = %v", s, err, valido)
		}
		if err != nil {
			if !errors.Is(err, ErrNomeVazio) {
				t.Errorf("NovoUsuario(%q) erro = %v, want ErrNomeVazio", s, err)
			}
		} else if u.Nome != aparado {
			t.Errorf("NovoUsuario(%q).Nome = %q, want %q", s, u.Nome, aparado)
		}
	})
}
