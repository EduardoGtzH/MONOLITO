#!/usr/bin/env bash
#
# pruebas_kafka.sh — verifica la mensajeria asincrona con Kafka, prueba por
# prueba. Cada prueba muestra el comando que ejecuta para poder repetirlo a
# mano; las mismas pruebas estan explicadas en el README.
#
#   ./pruebas_kafka.sh
#
# Requiere el sistema levantado (./setup.sh). Lo que inscribe lo da de baja
# y los lugares que aparta los devuelve; lo unico que queda es un alumno de
# prueba nuevo (matricula K + la hora), porque la API no borra alumnos.
#
set -uo pipefail
cd "$(dirname "$0")"

GW="${GW:-http://localhost:8000}"
KBIN=/opt/kafka/bin
[[ -f .env ]] && { set -a; . ./.env; set +a; }
PGUSER="${POSTGRES_USER:-admin}"

ROJO='\033[0;31m'; VERDE='\033[0;32m'; AZUL='\033[0;34m'; GRIS='\033[0;90m'; NC='\033[0m'
PASAN=0; FALLAN=0

titulo() { echo -e "\n${AZUL}########## $1 ##########${NC}"; }
pasa()   { echo -e "  ${VERDE}[PASA]${NC} $1"; PASAN=$((PASAN + 1)); }
falla()  { echo -e "  ${ROJO}[FALLA]${NC} $1"; FALLAN=$((FALLAN + 1)); }
cmd()    { echo -e "  ${GRIS}\$ $*${NC}"; }

# Las herramientas de linea de comandos vienen dentro del contenedor de Kafka.
kafka() { docker compose exec -T kafka "$KBIN/$1" --bootstrap-server localhost:9092 "${@:2}"; }

MATERIAS=(materias-1 materias-2 materias-3)
NOTIF=(notificaciones-1 notificaciones-2)

# Solo cuentan los logs escritos desde que empezo este script.
INICIO=$(date -u +%Y-%m-%dT%H:%M:%SZ)
lineas() { local patron=$1; shift; docker compose logs --since "$INICIO" "$@" 2>/dev/null | grep -F -- "$patron"; }

# espera_log PATRON SEGUNDOS CONTENEDORES... — Kafka es asincrono: el evento
# llega "en un momento", no en la misma peticion. Se revisa cada medio segundo.
espera_log() {
    local patron=$1 segundos=$2; shift 2
    for _ in $(seq 1 $((segundos * 2))); do
        lineas "$patron" "$@" > /dev/null && return 0
        sleep 0.5
    done
    return 1
}

campo_grupo() { curl -s "$GW/grupos/$1" | python3 -c "import sys,json; print(json.load(sys.stdin)['$2'])" 2>/dev/null; }
cupo_db() {
    docker compose exec -T db psql -U "$PGUSER" -d materias_db -Atc \
        "SELECT cupo_ocupado FROM grupos WHERE id = $1" 2>/dev/null | tr -d '\r'
}
# espera_cupo GRUPO VALOR SEGUNDOS — espera a que la base muestre ese cupo.
espera_cupo() {
    for _ in $(seq 1 $(($3 * 2))); do
        [[ "$(cupo_db "$1")" == "$2" ]] && return 0
        sleep 0.5
    done
    return 1
}
http() { curl -s -o /dev/null -w "%{http_code}" "$@"; }

# nginx resuelve los nombres de los backends SOLO al arrancar. Si despues se
# recrean o reinician los servicios, sus IPs pueden cambiar, y nginx sigue
# mandando a las viejas, que ahora pueden ser de OTRO contenedor: por eso
# salen 404 en rutas que si existen. Se revisa que cada ruta caiga en una
# instancia de su servicio (6 peticiones: dos vueltas de round robin) y, si
# no, se reinicia nginx para que vuelva a resolver los nombres.
gateway_bien() {
    local ruta servicio
    for par in "alumnos:alumnos" "materias:materias" "inscripciones?matricula=X:inscripciones"; do
        ruta=${par%%:*}; servicio=${par##*:}
        for _ in 1 2 3 4 5 6; do
            curl -s -D - -o /dev/null --max-time 5 "$GW/$ruta" \
                | grep -qi "^x-instance-id: $servicio-" || return 1
        done
    done
}
asegurar_gateway() {
    gateway_bien && return 0
    echo "  [!] nginx apunta a IPs viejas; se reinicia nginx-gateway"
    cmd "docker compose restart nginx-gateway"
    docker compose restart nginx-gateway > /dev/null 2>&1
    for _ in $(seq 1 10); do
        sleep 1
        gateway_bien && return 0
    done
    return 1
}

MAT="K$(date +%s)"   # alumno nuevo en cada corrida
GRUPO=6              # TC3045-01, viernes 10-12, 20 lugares

if ! asegurar_gateway; then
    echo -e "${ROJO}nginx no llega a los servicios en $GW. Revisa: docker compose ps${NC}"
    exit 1
fi

# ---------------------------------------------------------------------------
titulo "1. Topicos: 4, con 3 particiones cada uno"
cmd "docker compose exec kafka $KBIN/kafka-topics.sh --bootstrap-server localhost:9092 --describe"
desc=$(kafka kafka-topics.sh --describe 2>&1)
grep -E "^Topic:" <<< "$desc" | awk '{print "    " $1, $2, $5, $6}'
for t in alumnos.registrados inscripciones.creadas inscripciones.canceladas grupos.llenos; do
    n=$(grep -cE "Topic: $t[[:space:]]+Partition:" <<< "$desc")
    if [[ "$n" == 3 ]]; then pasa "$t: 3 particiones"; else falla "$t: $n particiones, se esperaban 3"; fi
done

# ---------------------------------------------------------------------------
titulo "2. Consumer groups: cada servicio que consume es una cola"
# describe_grupo GRUPO — la tabla de asignaciones. Reintenta mientras el
# grupo se esta rebalanceando.
describe_grupo() {
    local out
    for _ in $(seq 1 20); do
        out=$(kafka kafka-consumer-groups.sh --describe --group "$1" 2>&1)
        if ! grep -qiE "rebalancing|no active members" <<< "$out"; then break; fi
        sleep 1
    done
    echo "$out"
}
for g in materias notificaciones; do
    cmd "docker compose exec kafka $KBIN/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group $g"
    tabla=$(describe_grupo "$g")
    awk -v g="$g" '$1 == "GROUP" || $1 == g {printf "    %-26s %-9s %-4s %s\n", $2, $3, $6, $NF}' <<< "$tabla"
    filas=$(awk -v g="$g" '$1 == g && $NF != "-"' <<< "$tabla" | wc -l)
    clientes=$(awk -v g="$g" '$1 == g && $NF != "-" {print $NF}' <<< "$tabla" | sort -u | tr '\n' ' ')
    if [[ "$g" == materias ]]; then
        esperadas=3; instancias=3
    else
        esperadas=12; instancias=2
    fi
    if [[ "$filas" == "$esperadas" ]]; then
        pasa "$g: las $esperadas particiones tienen dueño"
    else
        falla "$g: $filas de $esperadas particiones con dueño"
    fi
    if [[ $(wc -w <<< "$clientes") == "$instancias" ]]; then
        pasa "$g: repartidas entre $instancias instancias ($clientes)"
    else
        falla "$g: se esperaban $instancias instancias y hay: $clientes"
    fi
done

# ---------------------------------------------------------------------------
titulo "3. alumnos.registrados: alumnos publica, notificaciones consume"
cmd "curl -X POST $GW/alumnos -d '{\"matricula\":\"$MAT\",\"nombre\":\"Prueba Kafka\",...}'"
c=$(http -X POST "$GW/alumnos" -H "Content-Type: application/json" \
    -d "{\"matricula\":\"$MAT\",\"nombre\":\"Prueba Kafka\",\"carrera\":\"Ingeniería en Sistemas\",\"semestre\":1}")
if [[ "$c" == 201 ]]; then pasa "alumno $MAT registrado (201)"; else falla "POST /alumnos devolvio $c"; fi
if espera_log "($MAT)" 15 "${NOTIF[@]}"; then
    pasa "notificaciones recibio el evento"
    lineas "($MAT)" "${NOTIF[@]}" | sed 's/^/    /'
else
    falla "notificaciones no recibio el evento de $MAT en 15 s"
fi

# ---------------------------------------------------------------------------
titulo "4. inscripciones.creadas"
cmd "curl -X POST $GW/inscripciones -d '{\"matricula\":\"$MAT\",\"grupo_id\":$GRUPO}'"
c=$(http -X POST "$GW/inscripciones" -H "Content-Type: application/json" \
    -d "{\"matricula\":\"$MAT\",\"grupo_id\":$GRUPO}")
if [[ "$c" == 201 ]]; then pasa "inscrito en el grupo $GRUPO (201)"; else falla "POST /inscripciones devolvio $c"; fi
if espera_log "$MAT quedo inscrito" 15 "${NOTIF[@]}"; then
    pasa "notificaciones recibio el evento"
    lineas "$MAT quedo inscrito" "${NOTIF[@]}" | sed 's/^/    /'
else
    falla "notificaciones no recibio inscripciones.creadas en 15 s"
fi

# ---------------------------------------------------------------------------
titulo "5. inscripciones.canceladas: dos grupos reciben el mismo evento"
antes=$(cupo_db $GRUPO)
[[ -n "$antes" ]] || { falla "no se pudo leer materias_db (docker compose exec db psql)"; antes=0; }
echo "    lugares ocupados en el grupo $GRUPO: $antes"
cmd "curl -X DELETE $GW/inscripciones/$MAT/$GRUPO"
c=$(http -X DELETE "$GW/inscripciones/$MAT/$GRUPO")
if [[ "$c" == 200 ]]; then pasa "baja registrada (200)"; else falla "DELETE devolvio $c"; fi

if espera_cupo $GRUPO $((antes - 1)) 15; then
    pasa "materias devolvio el lugar: $antes -> $((antes - 1))"
else
    falla "el cupo sigue en $(cupo_db $GRUPO), se esperaba $((antes - 1))"
fi
espera_log "por la baja de $MAT" 15 "${MATERIAS[@]}"
quien=$(lineas "por la baja de $MAT" "${MATERIAS[@]}")
if [[ $(grep -c . <<< "$quien") == 1 ]]; then
    pasa "grupo materias: lo proceso UNA sola instancia de las tres"
    sed 's/^/    /' <<< "$quien"
else
    falla "grupo materias: se esperaba 1 instancia y hubo $(grep -c . <<< "$quien")"
fi
if espera_log "$MAT se dio de baja" 15 "${NOTIF[@]}"; then
    pasa "grupo notificaciones: tambien recibio su copia"
    lineas "$MAT se dio de baja" "${NOTIF[@]}" | sed 's/^/    /'
else
    falla "notificaciones no recibio inscripciones.canceladas en 15 s"
fi

# ---------------------------------------------------------------------------
titulo "6. Idempotencia: el mismo evento, entregado otra vez"
# Se toma de Kafka el evento de la baja anterior y se vuelve a publicar tal
# cual, con el mismo evento_id. Es lo que pasa si un consumidor muere justo
# despues de procesar y antes de confirmar el offset.
cmd "docker compose exec kafka $KBIN/kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic inscripciones.canceladas --from-beginning --property print.key=true --property key.separator='|'"
evento=$(kafka kafka-console-consumer.sh --topic inscripciones.canceladas --from-beginning \
    --property print.key=true --property key.separator='|' --timeout-ms 5000 2>/dev/null \
    | grep -F "\"matricula\":\"$MAT\"" | head -1)
id=$(cut -d'|' -f2- <<< "$evento" | python3 -c "import sys,json; print(json.load(sys.stdin)['evento_id'])" 2>/dev/null)
if [[ -n "$id" ]]; then
    echo "    evento $id"
    antes=$(cupo_db $GRUPO)
    cmd "echo '<clave>|<json>' | docker compose exec -T kafka $KBIN/kafka-console-producer.sh --bootstrap-server localhost:9092 --topic inscripciones.canceladas --property parse.key=true --property key.separator='|'"
    printf '%s\n' "$evento" | kafka kafka-console-producer.sh --topic inscripciones.canceladas \
        --property parse.key=true --property key.separator='|'
    if espera_log "evento $id repetido" 15 "${MATERIAS[@]}"; then
        pasa "materias lo reconocio por su evento_id (tabla eventos_procesados)"
        lineas "evento $id repetido" "${MATERIAS[@]}" | sed 's/^/    /'
    else
        falla "materias no reporto el duplicado en 15 s"
    fi
    sleep 1
    despues=$(cupo_db $GRUPO)
    if [[ "$despues" == "$antes" ]]; then
        pasa "el lugar NO se devolvio dos veces (sigue en $despues)"
    else
        falla "el cupo cambio de $antes a $despues con un evento repetido"
    fi
    # notificaciones recuerda los eventos en memoria, por instancia: si el
    # duplicado cae en la otra, esa no lo conoce y manda el correo otra vez.
    if [[ -n "$(lineas "evento $id repetido" "${NOTIF[@]}")" ]]; then
        echo "    notificaciones: la misma instancia lo reconocio y no mando otro correo"
    elif (( $(lineas "$MAT se dio de baja" "${NOTIF[@]}" | grep -c .) >= 2 )); then
        echo "    notificaciones: cayo en la OTRA instancia, que no lo conocia, y mando"
        echo "    el correo otra vez (su memoria es por instancia; ver Limitaciones en el README)"
    fi
else
    falla "no se encontro en Kafka el evento de baja de $MAT"
fi

# ---------------------------------------------------------------------------
titulo "7. grupos.llenos: materias avisa cuando se ocupa el ultimo lugar"
G=2
read -r ocupado maximo nombre < <(curl -s "$GW/grupos/$G" | python3 -c \
    "import sys,json; g=json.load(sys.stdin); print(g['cupo_ocupado'], g['cupo_maximo'], g['materia_clave']+'-'+g['numero'])" 2>/dev/null)
if [[ -z "$ocupado" || -z "$maximo" ]]; then
    falla "no se pudo leer el grupo $G"
else
    echo "    grupo $G ($nombre): $ocupado de $maximo"
    # Si ya estaba lleno se libera uno, para poder llenarlo otra vez.
    if (( ocupado >= maximo )); then http -X POST "$GW/grupos/$G/liberar" > /dev/null; fi
    cmd "curl -X POST $GW/grupos/$G/apartar   (hasta llenarlo)"
    for _ in $(seq 1 "$maximo"); do
        actual=$(campo_grupo $G cupo_ocupado)
        [[ -z "$actual" ]] || (( actual >= maximo )) && break
        http -X POST "$GW/grupos/$G/apartar" > /dev/null
    done
    if espera_log "el grupo $nombre ya ocupo" 15 "${NOTIF[@]}"; then
        pasa "notificaciones recibio grupos.llenos"
        lineas "el grupo $nombre ya ocupo" "${NOTIF[@]}" | tail -1 | sed 's/^/    /'
    else
        falla "no llego grupos.llenos del grupo $G en 15 s"
    fi
    # Se devuelven los lugares hasta dejarlo como estaba.
    for _ in $(seq 1 "$maximo"); do
        actual=$(campo_grupo $G cupo_ocupado)
        [[ -z "$actual" ]] || (( actual <= ocupado )) && break
        http -X POST "$GW/grupos/$G/liberar" > /dev/null
    done
    echo "    restaurado: $(campo_grupo $G cupo_ocupado) de $maximo"
fi

# ---------------------------------------------------------------------------
titulo "8. Con materias apagado, la baja no falla: el evento espera en Kafka"
c=$(http -X POST "$GW/inscripciones" -H "Content-Type: application/json" \
    -d "{\"matricula\":\"$MAT\",\"grupo_id\":$GRUPO}")
[[ "$c" == 201 ]] || falla "no se pudo volver a inscribir a $MAT ($c)"
antes=$(cupo_db $GRUPO)
[[ -n "$antes" ]] || { falla "no se pudo leer materias_db (docker compose exec db psql)"; antes=0; }
cmd "docker compose stop ${MATERIAS[*]}"
docker compose stop "${MATERIAS[@]}" > /dev/null 2>&1

cmd "curl -X DELETE $GW/inscripciones/$MAT/$GRUPO"
c=$(http -X DELETE "$GW/inscripciones/$MAT/$GRUPO")
if [[ "$c" == 200 ]]; then
    pasa "la baja responde 200 aunque materias no esta"
else
    falla "DELETE devolvio $c con materias apagado"
fi
sleep 3
durante=$(cupo_db $GRUPO)
if [[ "$durante" == "$antes" ]]; then
    pasa "el lugar sigue ocupado ($durante): nadie ha leido el evento"
else
    falla "el cupo cambio a $durante sin materias corriendo"
fi
echo "    LAG = eventos que esperan en la cola del grupo materias:"
kafka kafka-consumer-groups.sh --describe --group materias 2>/dev/null \
    | awk '$1 == "GROUP" || $1 == "materias" {printf "      %-26s %-9s %s\n", $2, $3, $6}'

cmd "docker compose start ${MATERIAS[*]}"
docker compose start "${MATERIAS[@]}" > /dev/null 2>&1
if espera_cupo $GRUPO $((antes - 1)) 40; then
    pasa "materias regreso, leyo la cola y devolvio el lugar ($antes -> $((antes - 1)))"
    lineas "por la baja de $MAT" "${MATERIAS[@]}" | tail -1 | sed 's/^/    /'
else
    falla "materias regreso pero el cupo sigue en $(cupo_db $GRUPO)"
fi
# Al prender, materias pudo recibir otras IPs: nginx tiene que leerlas.
if asegurar_gateway; then
    pasa "nginx vuelve a llegar a las 3 instancias de materias"
else
    falla "nginx no llega a materias despues de reiniciarlas"
fi

# ---------------------------------------------------------------------------
echo ""
if (( FALLAN == 0 )); then
    echo -e "${VERDE}Las $PASAN pruebas pasaron.${NC}"
else
    echo -e "${ROJO}$FALLAN fallaron${NC}, $PASAN pasaron."
fi
(( FALLAN == 0 ))
