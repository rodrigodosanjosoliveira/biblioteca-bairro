package domain

import (
	"errors"
	"testing"
)

func TestNovoLivro(t *testing.T) {
	tests := []struct {
		name       string
		titulo     string
		wantTitulo string
		wantErr    error
	}{
		{name: "titulo valido", titulo: "Dom Casmurro", wantTitulo: "Dom Casmurro"},
		{name: "espacos em volta sao aparados", titulo: "  \tDom Casmurro \n", wantTitulo: "Dom Casmurro"},
		{name: "espacos internos preservados", titulo: " O  Alienista ", wantTitulo: "O  Alienista"},
		{name: "vazio", titulo: "", wantErr: ErrTituloVazio},
		{name: "so espacos", titulo: "   ", wantErr: ErrTituloVazio},
		{name: "so tab e newline", titulo: "\t\n\r ", wantErr: ErrTituloVazio},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NovoLivro(tt.titulo)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NovoLivro(%q) erro = %v, want %v", tt.titulo, err, tt.wantErr)
				}
				if got.ID != 0 || got.Titulo != "" {
					t.Errorf("NovoLivro(%q) em erro = %+v, want Livro{}", tt.titulo, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NovoLivro(%q) erro inesperado: %v", tt.titulo, err)
			}
			if got.Titulo != tt.wantTitulo {
				t.Errorf("NovoLivro(%q).Titulo = %q, want %q", tt.titulo, got.Titulo, tt.wantTitulo)
			}
			if got.ID != 0 {
				t.Errorf("NovoLivro(%q).ID = %d, want 0", tt.titulo, got.ID)
			}
		})
	}
}

func TestErrTituloVazioMensagem(t *testing.T) {
	if ErrTituloVazio == nil {
		t.Fatal("ErrTituloVazio nil")
	}
	if got, want := ErrTituloVazio.Error(), "livro: título vazio"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
