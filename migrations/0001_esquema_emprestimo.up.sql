CREATE TABLE livro (
    id     bigint GENERATED ALWAYS AS IDENTITY,
    titulo text   NOT NULL,
    CONSTRAINT livro_pkey PRIMARY KEY (id),
    CONSTRAINT livro_titulo_check CHECK (btrim(titulo) <> '')
);

CREATE TABLE usuario (
    id   bigint GENERATED ALWAYS AS IDENTITY,
    nome text   NOT NULL,
    CONSTRAINT usuario_pkey PRIMARY KEY (id),
    CONSTRAINT usuario_nome_check CHECK (btrim(nome) <> '')
);

CREATE TABLE emprestimo (
    id         bigint      GENERATED ALWAYS AS IDENTITY,
    livro_id   bigint      NOT NULL,
    usuario_id bigint      NOT NULL,
    inicio     timestamptz NOT NULL,
    vencimento date        NOT NULL,
    devolucao  timestamptz NULL,
    CONSTRAINT emprestimo_pkey PRIMARY KEY (id),
    CONSTRAINT emprestimo_livro_id_fkey FOREIGN KEY (livro_id) REFERENCES livro (id) ON DELETE RESTRICT,
    CONSTRAINT emprestimo_usuario_id_fkey FOREIGN KEY (usuario_id) REFERENCES usuario (id) ON DELETE RESTRICT,
    CONSTRAINT emprestimo_datas_finitas_check CHECK (isfinite(inicio) AND isfinite(vencimento) AND (devolucao IS NULL OR isfinite(devolucao)))
);

CREATE UNIQUE INDEX emprestimo_livro_aberto_uq ON emprestimo (livro_id) WHERE devolucao IS NULL;
CREATE INDEX emprestimo_livro_id_idx ON emprestimo (livro_id);
CREATE INDEX emprestimo_usuario_id_idx ON emprestimo (usuario_id);
