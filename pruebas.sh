#!/usr/bin/env bash
#
# pruebas.sh — verificación del sistema, escenario por escenario.
# No es una demo guiada: son comprobaciones reproducibles.
# El profesor puede correr esto, o probar a mano con los endpoints
# documentados en el README.
set -uo pipefail

AZUL='\033[0;34m'; VERDE='\033[0;32m'; AMARILLO='\033[1;33m'; NC='\033[0m'
NGINX="http://localhost:8000"    # puerta principal
GOLB="http://localhost:8001"     # load balancer de Go, para comparar
MW="http://localhost:8500"       # middleware

titulo() { echo -e "\n${AZUL}########## $1 ##########${NC}\n"; }
pausa()  { echo -e "\n${AMARILLO}[Enter para continuar]${NC}"; read -r; }

# El ancla ^ es necesaria: sin ella el grep también atrapa la línea
# Access-Control-Expose-Headers, que menciona X-Instance-Id como valor.
instancia() {
    curl -s -D - -o /dev/null "$1" | grep -i "^x-instance-id" | tr -d '\r'
}

titulo "1. Registro de servicios (middleware)"
curl -s "$MW/status" | python3 -m json.tool
pausa

titulo "2. Round robin por nginx en alumnos"
echo "Nueve peticiones consecutivas al :8000"
for i in $(seq 1 9); do instancia "$NGINX/alumnos"; done
pausa

titulo "3. Pools independientes"
echo "--- materias, su propio cursor ---"
for i in $(seq 1 6); do instancia "$NGINX/materias"; done
echo "--- /grupos cae en el mismo pool que /materias ---"
instancia "$NGINX/grupos/3"
echo "--- inscripciones, otro cursor más ---"
for i in $(seq 1 3); do instancia "$NGINX/inscripciones?matricula=A01234567"; done
pausa

titulo "4. Las dos implementaciones, lado a lado"
echo "--- nginx (:8000), round robin declarativo en 30 lineas de config ---"
for i in $(seq 1 6); do instancia "$NGINX/alumnos"; done
echo ""
echo "--- Go (:8001), round robin con atomic.AddUint64 ---"
for i in $(seq 1 6); do instancia "$GOLB/alumnos"; done
pausa

titulo "5. Chequeo pasivo contra chequeo activo"
echo "Apagando materias-2..."
docker compose stop materias-2 > /dev/null 2>&1
sleep 7

echo ""
echo "--- Por nginx: chequeo PASIVO. No sondea, espera a fallar. ---"
echo "Codigos de las proximas 6 peticiones:"
for i in $(seq 1 6); do curl -s -o /dev/null -w "%{http_code} " "$NGINX/materias"; done
echo ""
echo "(uno o dos 502 antes de que max_fails lo saque de rotacion)"

echo ""
echo "--- Por Go: chequeo ACTIVO. El middleware ya lo sabia. ---"
for i in $(seq 1 6); do curl -s -o /dev/null -w "%{http_code} " "$GOLB/materias"; done
echo ""
echo "(seis 200: la instancia caida nunca entro a la lista)"

echo ""
echo "Como lo ve el middleware:"
curl -s "$MW/status" | python3 -m json.tool | grep -A3 "materias-2"
pausa

titulo "6. Recuperación automática"
docker compose start materias-2 > /dev/null 2>&1
echo "Esperando al siguiente ciclo de heartbeat..."
sleep 10
for i in $(seq 1 6); do instancia "$GOLB/materias"; done
echo "(materias-2 vuelve al ciclo sin reiniciar nada)"
pausa

titulo "7. El middleware no está en el camino crítico"
docker compose stop middleware > /dev/null 2>&1
echo "Middleware apagado."
echo "nginx no lo usaba nunca (upstreams estaticos):"
curl -s -o /dev/null -w "  nginx  -> %{http_code}\n" "$NGINX/alumnos"
echo "El load balancer de Go sirve su cache de instancias sanas:"
curl -s -o /dev/null -w "  Go     -> %{http_code}\n" "$GOLB/alumnos"
docker compose start middleware > /dev/null 2>&1
sleep 3
pausa

titulo "8. Cadena de validaciones"
echo "--- Inscripción válida ---"
curl -s -X POST "$NGINX/inscripciones" -H "Content-Type: application/json" \
  -d '{"matricula":"A01234569","grupo_id":6}' | python3 -m json.tool

echo -e "\n--- Alumno inexistente (404) ---"
curl -s -w "\nstatus: %{http_code}\n" -X POST "$NGINX/inscripciones" \
  -H "Content-Type: application/json" -d '{"matricula":"A00000000","grupo_id":3}'

echo -e "\n--- Ya inscrito (409) ---"
curl -s -w "\nstatus: %{http_code}\n" -X POST "$NGINX/inscripciones" \
  -H "Content-Type: application/json" -d '{"matricula":"A01234569","grupo_id":6}'

echo -e "\n--- Sin cupo (409) ---"
curl -s -w "\nstatus: %{http_code}\n" -X POST "$NGINX/inscripciones" \
  -H "Content-Type: application/json" -d '{"matricula":"A01234568","grupo_id":4}'
pausa

titulo "9. El tráfico entre servicios también pasa por nginx"
echo "Una sola peticion del cliente genera varias por el gateway."
curl -s -o /dev/null -X POST "$NGINX/inscripciones" \
  -H "Content-Type: application/json" \
  -d '{"matricula":"A01234570","grupo_id":1}'
sleep 1
echo ""
docker compose logs --tail 6 nginx-gateway 2>/dev/null | grep -E "GET|POST"
echo ""
echo "Las lineas con Go-http-client vienen de inscripciones, no del cliente."
pausa

titulo "10. Concurrencia: 10 peticiones al último lugar"
echo "El grupo 4 esta lleno (2 de 2). Ninguna debe pasar:"
for i in $(seq 1 10); do
  curl -s -o /dev/null -w "%{http_code} " -X POST "$NGINX/grupos/4/apartar" &
done
wait
echo -e "\n\nTodos 409, gracias al SELECT ... FOR UPDATE en materias-service."
pausa

titulo "11. Registro dinámico en el middleware"
curl -s -X POST "$MW/register" -H "Content-Type: application/json" \
  -d '{"servicio":"alumnos","url":"http://alumnos-1:8080","prefijos":["/alumnos"]}' \
  | python3 -m json.tool
echo ""
echo "Idempotente: registrar una URL existente no la duplica."
echo "nginx no se entera de esto: sus upstreams son estaticos."
echo "Ese es el precio de usar configuracion declarativa."

echo -e "\n${VERDE}Pruebas terminadas.${NC}"
