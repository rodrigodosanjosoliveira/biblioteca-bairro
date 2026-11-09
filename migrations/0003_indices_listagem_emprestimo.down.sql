-- 0003 down: remove os índices da listagem filtrada. Não destrutivo em dado:
-- as consultas seguem corretas, só mais lentas. DROP INDEX toma ACCESS
-- EXCLUSIVE em emprestimo até o fim do arquivo (leituras também esperam).
DROP INDEX emprestimo_aberto_id_idx;
DROP INDEX emprestimo_livro_id_id_idx;
DROP INDEX emprestimo_usuario_id_id_idx;
