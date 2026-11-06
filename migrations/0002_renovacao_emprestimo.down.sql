-- 0002 down: remove o contador de renovações (destrutivo: perde o contador;
-- os vencimentos já estendidos permanecem). Reverter o binário antes.
ALTER TABLE emprestimo
    DROP CONSTRAINT emprestimo_renovacoes_check,
    DROP COLUMN renovacoes;
