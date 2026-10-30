-- Tabela de controle do executor de migracoes (internal/migrate): uma linha por
-- migracao aplicada. Este arquivo fica fora da raiz de migrations/ de proposito:
-- o executor o aplica por conta propria antes de qualquer migracao numerada.
-- aplicada_em grava o inicio da transacao que aplicou a migracao (default now()).
CREATE TABLE IF NOT EXISTS migracao_aplicada (
    versao      integer     NOT NULL,
    nome        text        NOT NULL,
    checksum    text        NOT NULL,
    aplicada_em timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT migracao_aplicada_pkey PRIMARY KEY (versao),
    CONSTRAINT migracao_aplicada_versao_check CHECK (versao > 0),
    CONSTRAINT migracao_aplicada_nome_check CHECK (nome ~ '^[a-z0-9_]+$'),
    CONSTRAINT migracao_aplicada_checksum_check CHECK (checksum ~ '^[0-9a-f]{64}$'),
    CONSTRAINT migracao_aplicada_aplicada_em_check CHECK (isfinite(aplicada_em))
);

COMMENT ON TABLE migracao_aplicada IS 'Registro das migracoes aplicadas pelo executor: uma linha por versao.';
COMMENT ON COLUMN migracao_aplicada.versao IS 'Numero NNNN do arquivo da migracao, positivo.';
COMMENT ON COLUMN migracao_aplicada.nome IS 'Nome do arquivo sem o numero e sem o sufixo .up.sql.';
COMMENT ON COLUMN migracao_aplicada.checksum IS 'SHA-256, em hexadecimal minusculo, dos bytes do arquivo .up.sql aplicado.';
COMMENT ON COLUMN migracao_aplicada.aplicada_em IS 'Inicio da transacao que aplicou a migracao (default now()).';
