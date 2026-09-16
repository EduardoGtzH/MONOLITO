\connect alumnos_db

CREATE TABLE alumnos (
    id                SERIAL PRIMARY KEY,
    matricula         VARCHAR(20)  NOT NULL UNIQUE,
    nombre            VARCHAR(120) NOT NULL,
    carrera           VARCHAR(80)  NOT NULL,
    semestre          INTEGER      NOT NULL CHECK (semestre BETWEEN 1 AND 12),
    creditos_cursados INTEGER      NOT NULL DEFAULT 0,
    activo            BOOLEAN      NOT NULL DEFAULT TRUE,
    creado_en         TIMESTAMP    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_alumnos_matricula ON alumnos(matricula);

INSERT INTO alumnos (matricula, nombre, carrera, semestre, creditos_cursados) VALUES
    ('A01234567', 'Ana Lucía Márquez',  'Ingeniería en Sistemas', 5, 180),
    ('A01234568', 'Bruno Hernández',    'Ingeniería en Sistemas', 3, 96),
    ('A01234569', 'Carla Domínguez',    'Ingeniería Industrial',  7, 264),
    ('A01234570', 'Diego Ramírez',      'Ingeniería en Sistemas', 1, 0);
