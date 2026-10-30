#!/usr/bin/env bash
#
# simulacro.sh — "dia de inscripciones": muchos alumnos peleando por pocos
# lugares al mismo tiempo.
#
# Usa el grupo SIM100-01 (Taller de Concurrencia, 10 lugares, sabado), que
# existe solo para esto. Con 40 alumnos por defecto:
#
#   1. Registra 40 alumnos nuevos
#   2. Los 40 piden el mismo grupo AL MISMO TIEMPO: deben entrar exactamente
#      10 (SELECT ... FOR UPDATE en materias) y repartidos entre instancias
#   3. Los 30 que no alcanzaron se forman en la lista de espera
#   4. 4 de los inscritos se dan de baja: por Kafka, materias devuelve los
#      lugares y la lista de espera mete a los 4 PRIMEROS de la fila
#   5. Revisa que todo cuadre (verificar_consistencia.sh)
#   6. Deja el grupo vacio otra vez
#
#   ./simulacro.sh          40 alumnos
#   ./simulacro.sh 80       los que quieras (mas de 10)
#
set -uo pipefail
cd "$(dirname "$0")"

GW="${GW:-http://localhost:8000}"
N="${1:-40}"
BAJAS=4
[[ -f .env ]] && { set -a; . ./.env; set +a; }
PGUSER="${POSTGRES_USER:-admin}"

ROJO='\033[0;31m'; VERDE='\033[0;32m'; AZUL='\033[0;34m'; GRIS='\033[0;90m'; NC='\033[0m'
PASAN=0; FALLAN=0
titulo() { echo -e "\n${AZUL}########## $1 ##########${NC}"; }
pasa()   { echo -e "  ${VERDE}[PASA]${NC} $1"; PASAN=$((PASAN + 1)); }
falla()  { echo -e "  ${ROJO}[FALLA]${NC} $1"; FALLAN=$((FALLAN + 1)); }
nota()   { echo -e "  ${GRIS}$1${NC}"; }

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
sql() { docker compose exec -T db psql -U "$PGUSER" -d "$1" -At -c "$2" 2>/dev/null | tr -d '\r'; }
json() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)"; }

# El id del grupo de simulacro sale del catalogo, no se asume.
GRUPO=$(curl -s "$GW/materias/SIM100" | json "d['grupos'][0]['id']" 2>/dev/null)
if [[ -z "$GRUPO" ]]; then
    echo -e "${ROJO}No existe la materia SIM100. Levanta el sistema con ./setup.sh --limpio${NC}"
    exit 1
fi
CUPO=$(curl -s "$GW/grupos/$GRUPO" | json "d['cupo_maximo']")
(( N > CUPO )) || { echo "Usa mas de $CUPO alumnos para que no alcancen los lugares"; exit 1; }

ocupados() { curl -s "$GW/grupos/$GRUPO" | json "d['cupo_ocupado']" 2>/dev/null; }

# Deja el grupo vacio: primero vacia la fila (si no, cada baja metera al
# siguiente de la fila) y despues da de baja a los inscritos.
limpiar() {
    for m in $(sql inscripciones_db "SELECT matricula FROM lista_espera WHERE grupo_id = $GRUPO AND estado = 'ESPERANDO'"); do
        curl -s -o /dev/null -X DELETE "$GW/lista-espera/$m/$GRUPO"
    done
    for m in $(sql inscripciones_db "SELECT matricula FROM inscripciones WHERE grupo_id = $GRUPO AND estado = 'ACTIVA'"); do
        curl -s -o /dev/null -X DELETE "$GW/inscripciones/$m/$GRUPO"
    done
    for _ in $(seq 1 30); do [[ "$(ocupados)" == 0 ]] && return 0; sleep 1; done
    return 1
}

if [[ "$(ocupados)" != 0 ]]; then
    nota "El grupo SIM100-01 tenia gente de una corrida anterior: se vacia primero."
    limpiar || { echo "No se pudo vaciar el grupo"; exit 1; }
fi

PREFIJO="S$(date +%s)"
MATS=(); for i in $(seq -w 1 "$N"); do MATS+=("$PREFIJO-$i"); done

# ---------------------------------------------------------------------------
titulo "1. Registrar $N alumnos"
printf '%s\n' "${MATS[@]}" | xargs -P 20 -I{} curl -s -o /dev/null -w "%{http_code}\n" \
    -X POST "$GW/alumnos" -H "Content-Type: application/json" \
    -d '{"matricula":"{}","nombre":"Alumno {}","carrera":"Ingeniería en Sistemas","semestre":1}' > "$TMP/alta"
ok=$(grep -c '^201$' "$TMP/alta")
if (( ok == N )); then pasa "$N alumnos registrados ($PREFIJO-01 a $PREFIJO-$N)"; else falla "solo $ok de $N registrados"; fi

# ---------------------------------------------------------------------------
titulo "2. Los $N piden el grupo SIM100-01 ($CUPO lugares) al mismo tiempo"
nota "\$ xargs -P $N curl -X POST $GW/inscripciones   (todas en paralelo)"
printf '%s\n' "${MATS[@]}" | xargs -P "$N" -I{} curl -s -o /dev/null \
    -w "{} %{http_code} %header{x-instance-id}\n" \
    -X POST "$GW/inscripciones" -H "Content-Type: application/json" \
    -d "{\"matricula\":\"{}\",\"grupo_id\":$GRUPO}" > "$TMP/inscribir"
entraron=$(awk '$2 == 201' "$TMP/inscribir" | wc -l)
rechazados=$(awk '$2 == 409' "$TMP/inscribir" | wc -l)
if (( entraron == CUPO )); then
    pasa "entraron exactamente $CUPO: ni uno de mas"
else
    falla "entraron $entraron, se esperaban $CUPO"
fi
if (( rechazados == N - CUPO )); then
    pasa "los otros $rechazados recibieron 409 (sin cupo)"
else
    falla "$rechazados rechazados con 409; esperados $((N - CUPO))"
    awk '$2 != 201 && $2 != 409' "$TMP/inscribir" | head -3 | sed 's/^/    /'
fi
o=$(ocupados)
if (( o == CUPO )); then pasa "materias dice $o/$CUPO ocupados"; else falla "materias dice $o ocupados"; fi
echo "    quien atendio las $N peticiones:"
awk '{print $3}' "$TMP/inscribir" | sort | uniq -c | sed 's/^/     /'

# ---------------------------------------------------------------------------
titulo "3. Los que no alcanzaron se forman en la lista de espera"
awk '$2 != 201 {print $1}' "$TMP/inscribir" > "$TMP/fuera"
xargs -P 20 -I{} curl -s -o /dev/null -w "%{http_code}\n" \
    -X POST "$GW/lista-espera" -H "Content-Type: application/json" \
    -d "{\"matricula\":\"{}\",\"grupo_id\":$GRUPO}" < "$TMP/fuera" > "$TMP/fila"
formados=$(grep -c '^201$' "$TMP/fila")
if (( formados == N - CUPO )); then pasa "$formados formados"; else falla "solo $formados de $((N - CUPO)) se formaron"; fi
curl -s "$GW/lista-espera?grupo_id=$GRUPO" | json "'\n'.join(f\"{l['posicion']} {l['matricula']}\" for l in d)" > "$TMP/orden"
pos=$(awk '{print $1}' "$TMP/orden" | tr '\n' ' ')
esperado=$(seq 1 $((N - CUPO)) | tr '\n' ' ')
if [[ "$pos" == "$esperado" ]]; then pasa "posiciones 1 a $((N - CUPO)), sin huecos ni repetidos"; else falla "posiciones raras: $pos"; fi

# ---------------------------------------------------------------------------
titulo "4. $BAJAS inscritos se dan de baja: la fila avanza sola por Kafka"
head -n "$BAJAS" "$TMP/orden" | awk '{print $2}' > "$TMP/siguientes"
awk '$2 == 201 {print $1}' "$TMP/inscribir" | head -n "$BAJAS" > "$TMP/salen"
nota "baja -> inscripciones.canceladas -> materias devuelve el lugar -> lugares.liberados -> inscripciones mete al primero de la fila"
xargs -P "$BAJAS" -I{} curl -s -o /dev/null -X DELETE "$GW/inscripciones/{}/$GRUPO" < "$TMP/salen"
for _ in $(seq 1 30); do
    dentro=0
    while read -r m; do
        [[ "$(curl -s "$GW/inscripciones?matricula=$m" | json "len(d)" 2>/dev/null)" == 1 ]] && dentro=$((dentro + 1))
    done < "$TMP/siguientes"
    (( dentro == BAJAS )) && break
    sleep 1
done
if (( dentro == BAJAS )); then
    pasa "entraron los $BAJAS PRIMEROS de la fila: $(tr '\n' ' ' < "$TMP/siguientes")"
else
    falla "de los $BAJAS primeros de la fila solo entraron $dentro"
fi
o=$(ocupados)
quedan=$(curl -s "$GW/lista-espera?grupo_id=$GRUPO" | json "len(d)")
if (( o == CUPO )); then pasa "el grupo volvio a quedar lleno: $o/$CUPO"; else falla "el grupo quedo en $o/$CUPO"; fi
if (( quedan == N - CUPO - BAJAS )); then pasa "en la fila quedan $quedan"; else falla "en la fila quedan $quedan, se esperaban $((N - CUPO - BAJAS))"; fi

# ---------------------------------------------------------------------------
titulo "5. ¿Cuadra todo?"
if ./verificar_consistencia.sh > "$TMP/consistencia" 2>&1; then
    pasa "verificar_consistencia.sh: $(tail -1 "$TMP/consistencia" | sed 's/\x1b\[[0-9;]*m//g')"
else
    falla "verificar_consistencia.sh encontro diferencias:"
    grep FALLA "$TMP/consistencia" | sed 's/^/    /'
fi

# ---------------------------------------------------------------------------
titulo "6. Dejar el grupo vacio"
if limpiar; then pasa "SIM100-01 otra vez en 0/$CUPO"; else falla "el grupo quedo en $(ocupados)/$CUPO"; fi
nota "Los $N alumnos $PREFIJO-* quedan registrados: la API no borra alumnos."

echo ""
if (( FALLAN == 0 )); then
    echo -e "${VERDE}Simulacro completo: $PASAN comprobaciones pasaron.${NC}"
else
    echo -e "${ROJO}$FALLAN fallaron${NC}, $PASAN pasaron."
fi
(( FALLAN == 0 ))
