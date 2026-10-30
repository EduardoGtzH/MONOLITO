\connect inscripciones_db

CREATE TABLE inscripciones (
    id            SERIAL PRIMARY KEY,
    matricula     VARCHAR(20) NOT NULL,
    grupo_id      INTEGER     NOT NULL,
    materia_clave VARCHAR(10) NOT NULL,
    estado        VARCHAR(12) NOT NULL DEFAULT 'ACTIVA'
                  CHECK (estado IN ('ACTIVA','DADA_DE_BAJA')),
    inscrito_en   TIMESTAMP   NOT NULL DEFAULT NOW(),
    baja_en       TIMESTAMP
);

CREATE UNIQUE INDEX idx_inscripcion_unica
    ON inscripciones(matricula, grupo_id)
    WHERE estado = 'ACTIVA';

CREATE INDEX idx_inscripciones_matricula ON inscripciones(matricula);

-- Lista de espera: alumnos formados para un grupo lleno. El orden de la fila
-- es el id (quien se formo primero). Cuando se libera un lugar, el servicio
-- inscribe al primero que siga ESPERANDO.
CREATE TABLE lista_espera (
    id          SERIAL PRIMARY KEY,
    matricula   VARCHAR(20)  NOT NULL,
    grupo_id    INTEGER      NOT NULL,
    materia_clave VARCHAR(10) NOT NULL,
    estado      VARCHAR(10)  NOT NULL DEFAULT 'ESPERANDO'
                CHECK (estado IN ('ESPERANDO','INSCRITO','CANCELADA','SALIO')),
    motivo      VARCHAR(200),
    formado_en  TIMESTAMP    NOT NULL DEFAULT NOW(),
    resuelto_en TIMESTAMP
);

-- Un alumno solo puede estar formado una vez en el mismo grupo.
CREATE UNIQUE INDEX idx_espera_unica
    ON lista_espera(matricula, grupo_id)
    WHERE estado = 'ESPERANDO';

CREATE INDEX idx_espera_grupo ON lista_espera(grupo_id, estado, id);
