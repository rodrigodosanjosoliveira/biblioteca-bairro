-- 0002: contador de renovações do empréstimo (DEL-011).
-- DEFAULT constante: as linhas existentes leem 0 sem reescrever a tabela.
-- O CHECK fixa o limite de negócio (2 renovações) como segunda barreira.
ALTER TABLE emprestimo
    ADD COLUMN renovacoes integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT emprestimo_renovacoes_check CHECK (renovacoes >= 0 AND renovacoes <= 2);
