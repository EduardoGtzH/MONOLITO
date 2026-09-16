#!/usr/bin/env bash
#
# demo.sh — recorre las pruebas del sistema una por una.
set -uo pipefail

AZUL='\033[0;34m'; VERDE='\033[0;32m'; AMARILLO='\033[1;33m'; NC='\033[0m'
LB="http://localhost:8000"
MW="http://localhost:8500"

titulo() { echo -e "\n${AZUL}########## $1 ##########${NC}\n"; }
pausa()  { echo -e "\n${AMARILLO}[Enter para continuar]${NC}"; read -r; }

instancia() {
    curl -s -D - -o /dev/null "$1" | grep -i "x-instance-id" | tr -d '\r'
}

titulo "1. Estado del registro (service discovery)"
curl -s "$MW/status" | python3 -m json.tool
pausa

titulo "2. Round robin en alumnos"
echo "Nueve peticiones consecutivas:"
for i in $(seq 1 9); do instancia "$LB/alumnos"; done
pausa

titulo "3. Round robin en materias (contador independiente)"
for i in $(seq 1 6); do instancia "$LB/materias"; done
pausa

titulo "4. Failover: apagando materias-2"
docker compose stop materias-2
echo "Esperando a que el middleware lo detecte..."
sleep 7
curl -s "$MW/status" | python3 -m json.tool | grep -B1 -A3 "materias-2"
echo -e "\nEl tráfico ahora solo va a materias-1 y materias-3:"
for i in $(seq 1 6); do instancia "$LB/materias"; done
pausa

titulo "5. Recuperación automática"
docker compose start materias-2
echo "Esperando al siguiente ciclo de heartbeat..."
sleep 8
for i in $(seq 1 6); do instancia "$LB/materias"; done
pausa

titulo "6. El middleware es prescindible para enrutar"
docker compose stop middleware
echo "Middleware apagado. El load balancer sirve su caché:"
curl -s -o /dev/null -w "status: %{http_code}\n" "$LB/alumnos"
instancia "$LB/alumnos"
docker compose start middleware
sleep 3
pausa

titulo "7. Cadena de validaciones"
echo "--- Inscripción válida ---"
curl -s -X POST "$LB/inscripciones" -H "Content-Type: application/json" \
  -d '{"matricula":"A01234569","grupo_id":6}' | python3 -m json.tool

echo -e "\n--- Alumno inexistente (404) ---"
curl -s -w "\nstatus: %{http_code}\n" -X POST "$LB/inscripciones" \
  -H "Content-Type: application/json" -d '{"matricula":"A00000000","grupo_id":3}'

echo -e "\n--- Ya inscrito (409) ---"
curl -s -w "\nstatus: %{http_code}\n" -X POST "$LB/inscripciones" \
  -H "Content-Type: application/json" -d '{"matricula":"A01234569","grupo_id":6}'

echo -e "\n--- Sin cupo (409) ---"
curl -s -w "\nstatus: %{http_code}\n" -X POST "$LB/inscripciones" \
  -H "Content-Type: application/json" -d '{"matricula":"A01234568","grupo_id":4}'
pausa

titulo "8. Concurrencia: 10 peticiones al último lugar"
echo "El grupo 4 tiene 2 de 2. Ninguna debe pasar:"
for i in $(seq 1 10); do
  curl -s -o /dev/null -w "%{http_code} " -X POST "$LB/grupos/4/apartar" &
done
wait
echo -e "\n\nTodos 409 gracias al SELECT ... FOR UPDATE."
pausa

titulo "9. Registro dinámico de un servicio"
curl -s -X POST "$MW/register" -H "Content-Type: application/json" \
  -d '{"servicio":"alumnos","url":"http://alumnos-1:8080","prefijos":["/alumnos"]}' \
  | python3 -m json.tool
echo -e "\n(idempotente: registrar una URL existente no la duplica)"

echo -e "\n${VERDE}Demo terminada.${NC}"
