#!/usr/bin/env python3
"""
generar_datos.py — genera los datos de prueba del sistema.

Escribe tres scripts SQL en db/init/ que Postgres corre al crear el volumen,
despues de los que definen las tablas:

    10-datos-alumnos.sql        200 alumnos generados y las materias que ya aprobaron
    11-datos-materias.sql       materias y grupos nuevos, y los prerrequisitos
    12-datos-inscripciones.sql  inscripciones que ya existian al arrancar

Lo importante: los lugares ocupados de cada grupo (cupo_ocupado, en
materias_db) salen de contar las inscripciones que se siembran aqui (en
inscripciones_db). Asi el sistema arranca CONSISTENTE y
verificar_consistencia.sh puede comprobar que lo sigue siendo.

Las inscripciones sembradas respetan las mismas reglas que la cadena de
validaciones: sin empalmes, sin repetir materia, con los prerrequisitos
aprobados y sin pasar el limite de creditos.

Es deterministico (semilla fija): correrlo dos veces da los mismos archivos.

    python3 db/generar_datos.py
"""
import os
import random
from datetime import datetime, timedelta

SEMILLA = 2026
MAX_CREDITOS = 30          # el mismo limite que usa inscripciones (MAX_CREDITOS)
ALUMNOS_GENERADOS = 200

AQUI = os.path.dirname(os.path.abspath(__file__))
SALIDA = os.path.join(AQUI, "init")

# ------------------------------------------------------------------ catalogo
# id, clave, nombre, creditos, carrera. Los 4 primeros ya estan en
# 03-materias.sql: aqui solo se repiten para conocer sus creditos.
MATERIAS = [
    (1, "TC1028", "Estructuras de Datos", 8, "Ingeniería en Sistemas"),
    (2, "TC2027", "Cómputo Distribuido", 8, "Ingeniería en Sistemas"),
    (3, "MA1001", "Cálculo Diferencial", 8, "Ingeniería en Sistemas"),
    (4, "TC3045", "Bases de Datos Avanzadas", 6, "Ingeniería en Sistemas"),
    (5, "TC1010", "Programación Orientada a Objetos", 8, "Ingeniería en Sistemas"),
    (6, "TC2005", "Sistemas Operativos", 8, "Ingeniería en Sistemas"),
    (7, "TC3050", "Redes de Computadoras", 6, "Ingeniería en Sistemas"),
    (8, "MA2002", "Cálculo Integral", 8, "Ingeniería en Sistemas"),
    (9, "MA1010", "Álgebra Lineal", 6, "Ingeniería en Sistemas"),
    (10, "TC4010", "Arquitectura de Software", 6, "Ingeniería en Sistemas"),
    (11, "IN2001", "Investigación de Operaciones", 6, "Ingeniería Industrial"),
    (12, "HU1001", "Ética Profesional", 4, "Tronco común"),
    (13, "SIM100", "Taller de Concurrencia", 4, "Tronco común"),
]
ORIGINALES = 4
CREDITOS = {m[1]: m[3] for m in MATERIAS}

# Solo las materias nuevas tienen prerrequisitos, para que las cuatro
# originales se comporten igual que siempre en pruebas.sh y pruebas_kafka.sh.
PRERREQUISITOS = {
    "TC2005": ["TC1028"],
    "TC3050": ["TC1028"],
    "MA2002": ["MA1001"],
    "TC4010": ["TC1010"],
    "IN2001": ["MA1001"],
}

# id, clave, numero, profesor, dia, inicio, fin, cupo_maximo, ocupados.
# Los 6 primeros ya estan en 03-materias.sql con estos mismos numeros; aqui se
# generan las inscripciones que los respaldan.
GRUPOS = [
    (1, "TC1028", "01", "Dr. Luis Fernando Peña", "LUN", "07:00", "09:00", 30, 12),
    (2, "TC1028", "02", "Dra. Mariana Solís", "MAR", "09:00", "11:00", 30, 29),
    (3, "TC2027", "01", "Dr. Ricardo Olvera", "LUN", "08:00", "10:00", 25, 5),
    (4, "TC2027", "02", "Dr. Ricardo Olvera", "MIE", "11:00", "13:00", 2, 2),
    (5, "MA1001", "01", "Mtra. Gabriela Ruiz", "JUE", "07:00", "09:00", 40, 18),
    (6, "TC3045", "01", "Dr. Javier Cantú", "VIE", "10:00", "12:00", 20, 3),
    (7, "TC1010", "01", "Mtra. Sofía Garza", "MAR", "07:00", "09:00", 35, 28),
    (8, "TC1010", "02", "Dr. Héctor Villarreal", "JUE", "15:00", "17:00", 35, 20),
    (9, "TC2005", "01", "Dra. Elena Treviño", "MIE", "07:00", "09:00", 30, 22),
    (10, "TC2005", "02", "Dra. Elena Treviño", "VIE", "13:00", "15:00", 30, 29),
    (11, "TC3050", "01", "Dr. Andrés Salinas", "LUN", "13:00", "15:00", 25, 12),
    (12, "TC3050", "02", "Dr. Andrés Salinas", "MIE", "17:00", "19:00", 25, 25),
    (13, "MA2002", "01", "Mtra. Gabriela Ruiz", "MAR", "11:00", "13:00", 40, 31),
    (14, "MA2002", "02", "Dr. Ramiro Cavazos", "JUE", "09:00", "11:00", 40, 15),
    (15, "MA1010", "01", "Dra. Patricia Leal", "LUN", "11:00", "13:00", 40, 26),
    (16, "MA1010", "02", "Dra. Patricia Leal", "VIE", "07:00", "09:00", 40, 33),
    (17, "TC4010", "01", "Dr. Javier Cantú", "JUE", "11:00", "13:00", 20, 9),
    (18, "IN2001", "01", "Mtro. Óscar Elizondo", "MAR", "15:00", "17:00", 30, 17),
    (19, "IN2001", "02", "Mtro. Óscar Elizondo", "VIE", "15:00", "17:00", 30, 8),
    (20, "HU1001", "01", "Lic. Teresa Montemayor", "MIE", "13:00", "15:00", 50, 35),
    (21, "HU1001", "02", "Lic. Teresa Montemayor", "SAB", "10:00", "12:00", 50, 12),
    # Grupo exclusivo de simulacro.sh: 10 lugares, vacio, en sabado para que
    # no choque con nada.
    (22, "SIM100", "01", "Dr. Ricardo Olvera", "SAB", "08:00", "10:00", 10, 0),
]
GRUPOS_ORIGINALES = 6

# Los 4 alumnos originales (02-alumnos.sql) no llevan inscripciones
# sembradas, para que las demos empiecen limpias. Si llevan materias
# aprobadas, para poder demostrar los prerrequisitos.
APROBADAS_ORIGINALES = {
    "A01234567": ["MA1001"],             # Ana: puede llevar TC1028 y TC2027 (el empalme)
    "A01234568": ["TC1028", "MA1001"],   # Bruno: ya puede llevar Sistemas Operativos
    "A01234569": ["MA1001"],             # Carla
    "A01234570": [],                     # Diego: primer semestre, sin prerrequisitos
}

NOMBRES = ["Alejandro", "Valeria", "Santiago", "Ximena", "Mateo", "Regina", "Sebastián",
           "Camila", "Diego", "Renata", "Emiliano", "Sofía", "Leonardo", "Fernanda",
           "Daniel", "Mariana", "Iker", "Andrea", "Rodrigo", "Paola", "Emilio", "Daniela",
           "Gael", "Natalia", "Javier", "Lucía", "Ángel", "Isabella", "Tomás", "Victoria"]
APELLIDOS = ["García", "Martínez", "López", "Hernández", "González", "Pérez", "Rodríguez",
             "Sánchez", "Ramírez", "Cruz", "Flores", "Gómez", "Morales", "Vázquez", "Reyes",
             "Jiménez", "Torres", "Díaz", "Gutiérrez", "Ruiz", "Mendoza", "Aguilar", "Ortiz",
             "Castillo", "Moreno", "Romero", "Herrera", "Medina", "Chávez", "Garza"]
CARRERAS = ["Ingeniería en Sistemas"] * 6 + ["Ingeniería Industrial"] * 3 + ["Ingeniería Mecatrónica"]


def minutos(hhmm):
    h, m = hhmm.split(":")
    return int(h) * 60 + int(m)


def se_empalman(a, b):
    """Igual que seEmpalman en validador.go: mismo dia y los rangos se cruzan."""
    return a[4] == b[4] and minutos(a[5]) < minutos(b[6]) and minutos(b[5]) < minutos(a[6])


def aprobadas_por_semestre(rnd, semestre):
    """Que materias del catalogo ya aprobo un alumno, segun su semestre."""
    aprobadas = []
    def tal_vez(clave, prob):
        if rnd.random() < prob:
            aprobadas.append(clave)
    if semestre >= 2:
        tal_vez("MA1001", 0.75)
        tal_vez("TC1010", 0.45)
    if semestre >= 3:
        tal_vez("TC1028", 0.70)
        tal_vez("MA1010", 0.50)
    if semestre >= 5:
        if "MA1001" in aprobadas:
            tal_vez("MA2002", 0.55)
        if "TC1028" in aprobadas:
            tal_vez("TC2005", 0.45)
        tal_vez("HU1001", 0.40)
    if semestre >= 7:
        tal_vez("TC3045", 0.35)
    return aprobadas


def sql_texto(s):
    return "'" + s.replace("'", "''") + "'"


def main():
    rnd = random.Random(SEMILLA)

    # ---------------------------------------------------------- alumnos
    alumnos = []  # (matricula, nombre, carrera, semestre, creditos_cursados, aprobadas)
    usados = set()
    for n in range(1, ALUMNOS_GENERADOS + 1):
        while True:
            nombre = f"{rnd.choice(NOMBRES)} {rnd.choice(APELLIDOS)} {rnd.choice(APELLIDOS)}"
            if nombre not in usados:
                usados.add(nombre)
                break
        semestre = rnd.randint(1, 9)
        aprobadas = aprobadas_por_semestre(rnd, semestre)
        # Creditos de materias de este catalogo, mas los de otras que no estan.
        cursados = sum(CREDITOS[c] for c in aprobadas) + (semestre - 1) * rnd.randint(28, 36)
        alumnos.append((f"A0200{n:04d}", nombre, rnd.choice(CARRERAS), semestre, cursados, aprobadas))

    # ---------------------------------------------------------- inscripciones
    # Para cada alumno: los grupos que ya lleva y sus creditos.
    horario = {a[0]: [] for a in alumnos}
    aprobadas = {a[0]: set(a[5]) for a in alumnos}
    inscripciones = []  # (matricula, grupo_id, clave)

    def puede(matricula, grupo):
        clave = grupo[1]
        if clave in aprobadas[matricula]:
            return False                      # ya la aprobo
        if any(g[1] == clave for g in horario[matricula]):
            return False                      # ya lleva otro grupo de esa materia
        if any(se_empalman(grupo, g) for g in horario[matricula]):
            return False                      # empalme
        if any(r not in aprobadas[matricula] for r in PRERREQUISITOS.get(clave, [])):
            return False                      # le falta un prerrequisito
        creditos = sum(CREDITOS[g[1]] for g in horario[matricula]) + CREDITOS[clave]
        return creditos <= MAX_CREDITOS       # limite de creditos

    # Primero los grupos mas llenos, que son los dificiles de completar.
    for grupo in sorted(GRUPOS, key=lambda g: -g[8]):
        candidatos = [a[0] for a in alumnos]
        rnd.shuffle(candidatos)
        elegidos = [m for m in candidatos if puede(m, grupo)][:grupo[8]]
        if len(elegidos) < grupo[8]:
            raise SystemExit(f"no alcanzan los alumnos para el grupo {grupo[0]} ({grupo[1]}-{grupo[2]})")
        for m in elegidos:
            horario[m].append(grupo)
            inscripciones.append((m, grupo[0], grupo[1]))

    # Fechas de inscripcion repartidas en la semana anterior al arranque.
    base = datetime(2026, 10, 19, 8, 0)
    inscripciones.sort(key=lambda i: (i[1], i[0]))
    fechas = [base + timedelta(minutes=rnd.randint(0, 6 * 24 * 60)) for _ in inscripciones]

    # ---------------------------------------------------------- archivos
    cab = "-- Generado por db/generar_datos.py. No lo edites a mano: cambia el\n-- generador y vuelve a correrlo.\n"

    with open(os.path.join(SALIDA, "10-datos-alumnos.sql"), "w") as f:
        f.write(cab + "\\connect alumnos_db\n\n")
        f.write("INSERT INTO alumnos (matricula, nombre, carrera, semestre, creditos_cursados) VALUES\n")
        f.write(",\n".join(f"    ({sql_texto(a[0])}, {sql_texto(a[1])}, {sql_texto(a[2])}, {a[3]}, {a[4]})"
                           for a in alumnos) + ";\n\n")
        filas = [(m, c) for m, cs in APROBADAS_ORIGINALES.items() for c in cs]
        filas += [(a[0], c) for a in alumnos for c in a[5]]
        f.write("INSERT INTO materias_aprobadas (matricula, materia_clave) VALUES\n")
        f.write(",\n".join(f"    ({sql_texto(m)}, {sql_texto(c)})" for m, c in filas) + ";\n")

    with open(os.path.join(SALIDA, "11-datos-materias.sql"), "w") as f:
        f.write(cab + "\\connect materias_db\n\n")
        f.write("INSERT INTO materias (id, clave, nombre, creditos, carrera) VALUES\n")
        f.write(",\n".join(f"    ({m[0]}, {sql_texto(m[1])}, {sql_texto(m[2])}, {m[3]}, {sql_texto(m[4])})"
                           for m in MATERIAS[ORIGINALES:]) + ";\n")
        f.write("SELECT setval('materias_id_seq', (SELECT MAX(id) FROM materias));\n\n")
        ids = {m[1]: m[0] for m in MATERIAS}
        f.write("INSERT INTO grupos (id, materia_id, numero, profesor, dia, hora_inicio, hora_fin, cupo_maximo, cupo_ocupado) VALUES\n")
        f.write(",\n".join(f"    ({g[0]}, {ids[g[1]]}, {sql_texto(g[2])}, {sql_texto(g[3])}, {sql_texto(g[4])}, "
                           f"{sql_texto(g[5])}, {sql_texto(g[6])}, {g[7]}, {g[8]})"
                           for g in GRUPOS[GRUPOS_ORIGINALES:]) + ";\n")
        f.write("SELECT setval('grupos_id_seq', (SELECT MAX(id) FROM grupos));\n\n")
        pares = [(m, r) for m, rs in PRERREQUISITOS.items() for r in rs]
        f.write("INSERT INTO prerrequisitos (materia_id, requiere_id) VALUES\n")
        for i, (m, r) in enumerate(pares):
            fin = ";" if i == len(pares) - 1 else ","
            f.write(f"    ({ids[m]}, {ids[r]}){fin}  -- {m} requiere {r}\n")

    with open(os.path.join(SALIDA, "12-datos-inscripciones.sql"), "w") as f:
        f.write(cab + "\\connect inscripciones_db\n\n")
        f.write("INSERT INTO inscripciones (matricula, grupo_id, materia_clave, inscrito_en) VALUES\n")
        f.write(",\n".join(f"    ({sql_texto(m)}, {g}, {sql_texto(c)}, '{d:%Y-%m-%d %H:%M}')"
                           for (m, g, c), d in zip(inscripciones, fechas)) + ";\n")

    por_grupo = {}
    for _, g, _ in inscripciones:
        por_grupo[g] = por_grupo.get(g, 0) + 1
    for g in GRUPOS:
        assert por_grupo.get(g[0], 0) == g[8], g
    print(f"{len(alumnos)} alumnos, {len(MATERIAS)} materias, {len(GRUPOS)} grupos, "
          f"{len(inscripciones)} inscripciones sembradas (cuadran con cupo_ocupado)")


if __name__ == "__main__":
    main()
