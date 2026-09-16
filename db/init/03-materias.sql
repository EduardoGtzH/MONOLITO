\connect materias_db

CREATE TABLE materias (
    id       SERIAL PRIMARY KEY,
    clave    VARCHAR(10)  NOT NULL UNIQUE,
    nombre   VARCHAR(120) NOT NULL,
    creditos INTEGER      NOT NULL CHECK (creditos > 0),
    carrera  VARCHAR(80)  NOT NULL
);

CREATE TABLE grupos (
    id           SERIAL PRIMARY KEY,
    materia_id   INTEGER     NOT NULL REFERENCES materias(id) ON DELETE CASCADE,
    numero       VARCHAR(5)  NOT NULL,
    profesor     VARCHAR(120) NOT NULL,
    dia          VARCHAR(3)  NOT NULL CHECK (dia IN ('LUN','MAR','MIE','JUE','VIE','SAB')),
    hora_inicio  TIME        NOT NULL,
    hora_fin     TIME        NOT NULL,
    cupo_maximo  INTEGER     NOT NULL CHECK (cupo_maximo > 0),
    cupo_ocupado INTEGER     NOT NULL DEFAULT 0,
    UNIQUE (materia_id, numero),
    CHECK (hora_fin > hora_inicio)
);

CREATE INDEX idx_grupos_materia ON grupos(materia_id);

INSERT INTO materias (clave, nombre, creditos, carrera) VALUES
    ('TC1028', 'Estructuras de Datos',      8, 'Ingeniería en Sistemas'),
    ('TC2027', 'Cómputo Distribuido',       8, 'Ingeniería en Sistemas'),
    ('MA1001', 'Cálculo Diferencial',       8, 'Ingeniería en Sistemas'),
    ('TC3045', 'Bases de Datos Avanzadas',  6, 'Ingeniería en Sistemas');

INSERT INTO grupos (materia_id, numero, profesor, dia, hora_inicio, hora_fin, cupo_maximo, cupo_ocupado) VALUES
    (1, '01', 'Dr. Luis Fernando Peña',  'LUN', '07:00', '09:00', 30, 12),
    (1, '02', 'Dra. Mariana Solís',      'MAR', '09:00', '11:00', 30, 29),
    (2, '01', 'Dr. Ricardo Olvera',      'LUN', '08:00', '10:00', 25,  5),
    (2, '02', 'Dr. Ricardo Olvera',      'MIE', '11:00', '13:00',  2,  2),
    (3, '01', 'Mtra. Gabriela Ruiz',     'JUE', '07:00', '09:00', 40, 18),
    (4, '01', 'Dr. Javier Cantú',        'VIE', '10:00', '12:00', 20,  3);
