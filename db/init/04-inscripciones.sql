\connect inscripciones_db

CREATE TABLE inscripciones (
    id            SERIAL PRIMARY KEY,
    matricula     VARCHAR(20) NOT NULL,
    grupo_id      INTEGER     NOT NULL,
    materia_clave VARCHAR(10) NOT NULL,
    estado        VARCHAR(12) NOT NULL DEFAULT 'ACTIVA'
                  CHECK (estado IN ('ACTIVA','DADA_DE_BAJA')),
    inscrito_en   TIMESTAMP   NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_inscripcion_unica
    ON inscripciones(matricula, grupo_id)
    WHERE estado = 'ACTIVA';

CREATE INDEX idx_inscripciones_matricula ON inscripciones(matricula);
