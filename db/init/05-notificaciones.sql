\connect notificaciones_db

-- Cada aviso que manda el servicio de notificaciones. evento_id es UNIQUE:
-- si Kafka entrega el mismo evento dos veces, el segundo INSERT no hace nada,
-- sin importar cual de las dos instancias lo reciba.
CREATE TABLE notificaciones (
    id        SERIAL PRIMARY KEY,
    evento_id VARCHAR(36) NOT NULL UNIQUE,
    matricula VARCHAR(20),          -- NULL: el aviso es para coordinacion
    topico    VARCHAR(40) NOT NULL,
    mensaje   TEXT        NOT NULL,
    creada_en TIMESTAMP   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notificaciones_matricula ON notificaciones(matricula, id DESC);
