#!/usr/bin/env bash
#
# verificar_consistencia.sh — revisa que los datos de los servicios cuadren
# entre si.
#
# Cada servicio tiene su propia base, y entre bases no hay transacciones ni
# FOREIGN KEY: lo unico que las mantiene de acuerdo son las llamadas HTTP y
# los eventos de Kafka. Este script lo comprueba con los datos reales:
#
#   1. Kafka no tiene eventos pendientes (si los hay, se espera un poco)
#   2. El cupo ocupado de cada grupo (materias_db) es igual al numero de
#      inscripciones activas en ese grupo (inscripciones_db)
#   3. Ningun grupo tiene mas inscritos que lugares
#   4. Nadie lleva dos grupos de la misma materia
#   5. Nadie esta inscrito y formado en la fila del mismo grupo a la vez
#
# Sirve despues de cualquier prueba (pruebas.sh, pruebas_kafka.sh,
# simulacro.sh) o de apagar y prender instancias a mano.
#
#   ./verificar_consistencia.sh
#
set -uo pipefail
cd "$(dirname "$0")"

[[ -f .env ]] && { set -a; . ./.env; set +a; }
PGUSER="${POSTGRES_USER:-admin}"

ROJO='\033[0;31m'; VERDE='\033[0;32m'; AZUL='\033[0;34m'; NC='\033[0m'
PASAN=0; FALLAN=0
titulo() { echo -e "\n${AZUL}########## $1 ##########${NC}"; }
pasa()   { echo -e "  ${VERDE}[PASA]${NC} $1"; PASAN=$((PASAN + 1)); }
falla()  { echo -e "  ${ROJO}[FALLA]${NC} $1"; FALLAN=$((FALLAN + 1)); }

sql() { docker compose exec -T db psql -U "$PGUSER" -d "$1" -AtF'|' -c "$2" 2>/dev/null | tr -d '\r'; }

# Suma la columna LAG de un consumer group ("-" cuenta como 0).
lag_de() {
    docker compose exec -T kafka /opt/kafka/bin/kafka-consumer-groups.sh \
        --bootstrap-server localhost:9092 --describe --group "$1" 2>/dev/null \
        | awk -v g="$1" '$1 == g && $6 ~ /^[0-9]+$/ {s += $6} END {print s + 0}'
}

# ---------------------------------------------------------------------------
titulo "1. Kafka sin eventos pendientes"
# Una baja recien hecha todavia puede estar en camino: el cupo se corrige
# cuando materias lee el evento. Se espera hasta 20 s a que las colas se vacien.
for _ in $(seq 1 20); do
    pendientes=0
    for g in materias inscripciones notificaciones; do
        pendientes=$((pendientes + $(lag_de "$g")))
    done
    (( pendientes == 0 )) && break
    sleep 1
done
for g in materias inscripciones notificaciones; do
    l=$(lag_de "$g")
    if (( l == 0 )); then pasa "grupo $g: 0 eventos pendientes"; else falla "grupo $g: $l eventos pendientes"; fi
done

# ---------------------------------------------------------------------------
titulo "2. Cupo ocupado = inscripciones activas, grupo por grupo"
cupos=$(sql materias_db "SELECT g.id, m.clave || '-' || g.numero, g.cupo_ocupado, g.cupo_maximo
                         FROM grupos g JOIN materias m ON m.id = g.materia_id ORDER BY g.id")
activas=$(sql inscripciones_db "SELECT grupo_id, COUNT(*) FROM inscripciones
                                WHERE estado = 'ACTIVA' GROUP BY grupo_id")
if [[ -z "$cupos" ]]; then
    falla "no se pudo leer materias_db (docker compose exec db psql)"
else
    malos=0
    while IFS='|' read -r id nombre ocupado maximo; do
        reales=$(awk -F'|' -v id="$id" '$1 == id {print $2}' <<< "$activas")
        reales=${reales:-0}
        if [[ "$ocupado" != "$reales" ]]; then
            falla "grupo $id ($nombre): materias dice $ocupado ocupados, inscripciones tiene $reales activas"
            malos=$((malos + 1))
        fi
    done <<< "$cupos"
    total=$(grep -c . <<< "$cupos")
    (( malos == 0 )) && pasa "los $total grupos cuadran"
fi

# ---------------------------------------------------------------------------
titulo "3. Sin sobrecupo"
sobre=$(sql materias_db "SELECT id, cupo_ocupado, cupo_maximo FROM grupos
                         WHERE cupo_ocupado > cupo_maximo OR cupo_ocupado < 0")
if [[ -z "$sobre" ]]; then pasa "ningun grupo pasa de su cupo maximo"; else falla "grupos fuera de rango: $sobre"; fi

# ---------------------------------------------------------------------------
titulo "4. Nadie lleva dos grupos de la misma materia"
dobles=$(sql inscripciones_db "SELECT matricula, materia_clave FROM inscripciones
                               WHERE estado = 'ACTIVA' GROUP BY 1, 2 HAVING COUNT(*) > 1")
if [[ -z "$dobles" ]]; then pasa "sin materias repetidas"; else falla "materias repetidas: $dobles"; fi

# ---------------------------------------------------------------------------
titulo "5. Nadie inscrito y formado a la vez en el mismo grupo"
ambos=$(sql inscripciones_db "SELECT f.matricula, f.grupo_id FROM lista_espera f
                              JOIN inscripciones i ON i.matricula = f.matricula
                               AND i.grupo_id = f.grupo_id AND i.estado = 'ACTIVA'
                              WHERE f.estado = 'ESPERANDO'")
if [[ -z "$ambos" ]]; then pasa "la lista de espera y las inscripciones no se contradicen"; else falla "inscritos que siguen en la fila: $ambos"; fi

echo ""
if (( FALLAN == 0 )); then
    echo -e "${VERDE}Todo cuadra: $PASAN comprobaciones.${NC}"
else
    echo -e "${ROJO}$FALLAN no cuadran${NC}, $PASAN si."
fi
(( FALLAN == 0 ))
