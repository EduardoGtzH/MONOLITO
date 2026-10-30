-- Generado por db/generar_datos.py. No lo edites a mano: cambia el
-- generador y vuelve a correrlo.
\connect materias_db

INSERT INTO materias (id, clave, nombre, creditos, carrera) VALUES
    (5, 'TC1010', 'Programación Orientada a Objetos', 8, 'Ingeniería en Sistemas'),
    (6, 'TC2005', 'Sistemas Operativos', 8, 'Ingeniería en Sistemas'),
    (7, 'TC3050', 'Redes de Computadoras', 6, 'Ingeniería en Sistemas'),
    (8, 'MA2002', 'Cálculo Integral', 8, 'Ingeniería en Sistemas'),
    (9, 'MA1010', 'Álgebra Lineal', 6, 'Ingeniería en Sistemas'),
    (10, 'TC4010', 'Arquitectura de Software', 6, 'Ingeniería en Sistemas'),
    (11, 'IN2001', 'Investigación de Operaciones', 6, 'Ingeniería Industrial'),
    (12, 'HU1001', 'Ética Profesional', 4, 'Tronco común'),
    (13, 'SIM100', 'Taller de Concurrencia', 4, 'Tronco común');
SELECT setval('materias_id_seq', (SELECT MAX(id) FROM materias));

INSERT INTO grupos (id, materia_id, numero, profesor, dia, hora_inicio, hora_fin, cupo_maximo, cupo_ocupado) VALUES
    (7, 5, '01', 'Mtra. Sofía Garza', 'MAR', '07:00', '09:00', 35, 28),
    (8, 5, '02', 'Dr. Héctor Villarreal', 'JUE', '15:00', '17:00', 35, 20),
    (9, 6, '01', 'Dra. Elena Treviño', 'MIE', '07:00', '09:00', 30, 22),
    (10, 6, '02', 'Dra. Elena Treviño', 'VIE', '13:00', '15:00', 30, 29),
    (11, 7, '01', 'Dr. Andrés Salinas', 'LUN', '13:00', '15:00', 25, 12),
    (12, 7, '02', 'Dr. Andrés Salinas', 'MIE', '17:00', '19:00', 25, 25),
    (13, 8, '01', 'Mtra. Gabriela Ruiz', 'MAR', '11:00', '13:00', 40, 31),
    (14, 8, '02', 'Dr. Ramiro Cavazos', 'JUE', '09:00', '11:00', 40, 15),
    (15, 9, '01', 'Dra. Patricia Leal', 'LUN', '11:00', '13:00', 40, 26),
    (16, 9, '02', 'Dra. Patricia Leal', 'VIE', '07:00', '09:00', 40, 33),
    (17, 10, '01', 'Dr. Javier Cantú', 'JUE', '11:00', '13:00', 20, 9),
    (18, 11, '01', 'Mtro. Óscar Elizondo', 'MAR', '15:00', '17:00', 30, 17),
    (19, 11, '02', 'Mtro. Óscar Elizondo', 'VIE', '15:00', '17:00', 30, 8),
    (20, 12, '01', 'Lic. Teresa Montemayor', 'MIE', '13:00', '15:00', 50, 35),
    (21, 12, '02', 'Lic. Teresa Montemayor', 'SAB', '10:00', '12:00', 50, 12),
    (22, 13, '01', 'Dr. Ricardo Olvera', 'SAB', '08:00', '10:00', 10, 0);
SELECT setval('grupos_id_seq', (SELECT MAX(id) FROM grupos));

INSERT INTO prerrequisitos (materia_id, requiere_id) VALUES
    (6, 1),  -- TC2005 requiere TC1028
    (7, 1),  -- TC3050 requiere TC1028
    (8, 3),  -- MA2002 requiere MA1001
    (10, 5),  -- TC4010 requiere TC1010
    (11, 3);  -- IN2001 requiere MA1001
