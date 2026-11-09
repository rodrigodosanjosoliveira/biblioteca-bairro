-- 0003: índices da listagem filtrada de empréstimos (DEL-015, D-21).
-- Keyset por id dentro de usuario_id e de livro_id, e por id entre os
-- abertos (situacao=aberto e vencido; vencido filtra vencimento sobre eles).
-- Aditiva: os índices simples da 0001 ficam (remoção em migração futura).
-- Construção comum, sem a opção online (D-16 G7): lock SHARE em emprestimo,
-- leituras seguem e escritas esperam até o fim do arquivo.
CREATE INDEX emprestimo_usuario_id_id_idx ON emprestimo (usuario_id, id);
CREATE INDEX emprestimo_livro_id_id_idx ON emprestimo (livro_id, id);
CREATE INDEX emprestimo_aberto_id_idx ON emprestimo (id) WHERE devolucao IS NULL;
