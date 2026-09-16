#!/usr/bin/env bash
#
# setup.sh — instala dependencias y levanta el sistema completo.
# Uso:  ./setup.sh          instala si falta y levanta
#       ./setup.sh --limpio borra volúmenes y reconstruye desde cero
#
set -euo pipefail

# set -e corta al primer error, -u truena si usas una variable no definida,
# y -o pipefail hace que un pipe falle si cualquier etapa falla, no solo la
# última. Sin esto un error a media instalación pasaría desapercibido.

ROJO='\033[0;31m'; VERDE='\033[0;32m'; AZUL='\033[0;34m'; AMARILLO='\033[1;33m'; NC='\033[0m'
paso()  { echo -e "\n${AZUL}==> $1${NC}"; }
ok()    { echo -e "${VERDE}  [OK] $1${NC}"; }
aviso() { echo -e "${AMARILLO}  [!] $1${NC}"; }
error() { echo -e "${ROJO}  [X] $1${NC}"; exit 1; }

cd "$(dirname "$0")"

LIMPIO=false
[[ "${1:-}" == "--limpio" ]] && LIMPIO=true

# ---------------------------------------------------------------- 1. Docker
paso "Verificando Docker"

if ! command -v docker &> /dev/null; then
    aviso "Docker no está instalado. Instalando..."

    sudo apt update
    sudo apt install -y ca-certificates curl gnupg git

    sudo install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/debian/gpg \
        | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    sudo chmod a+r /etc/apt/keyrings/docker.gpg

    # Kali es rolling release y no tiene canal propio en el repo de Docker.
    # Apuntamos al de Debian estable, que es compatible.
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
https://download.docker.com/linux/debian bookworm stable" \
        | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

    sudo apt update
    sudo apt install -y docker-ce docker-ce-cli containerd.io \
        docker-buildx-plugin docker-compose-plugin

    sudo systemctl enable --now docker
    sudo usermod -aG docker "$USER"

    aviso "Se agregó tu usuario al grupo docker."
    aviso "Cierra sesión y vuelve a entrar, o corre: newgrp docker"
else
    ok "Docker $(docker --version | grep -oP '\d+\.\d+\.\d+' | head -1)"
fi

if ! docker compose version &> /dev/null; then
    error "Falta el plugin de Compose. Instala docker-compose-plugin."
fi
ok "Compose $(docker compose version --short)"

if ! docker info &> /dev/null; then
    error "No se puede hablar con el daemon. Corre: newgrp docker"
fi
ok "Daemon accesible"

# ---------------------------------------------------------------- 2. Variables
paso "Configurando variables de entorno"

if [[ ! -f .env ]]; then
    cp .env.example .env
    ok "Archivo .env creado desde .env.example"
else
    ok "Archivo .env ya existe"
fi

# ---------------------------------------------------------------- 3. Puertos
paso "Verificando puertos"

PUERTOS=(3000 8000 8500 5432)
OCUPADOS=()
for p in "${PUERTOS[@]}"; do
    if ss -ltn 2>/dev/null | grep -q ":$p "; then
        OCUPADOS+=("$p")
    fi
done

if [[ ${#OCUPADOS[@]} -gt 0 ]]; then
    aviso "Puertos ocupados: ${OCUPADOS[*]}"
    aviso "Si son de una corrida previa está bien; si no, libéralos."
else
    ok "Puertos libres"
fi

# ---------------------------------------------------------------- 4. Levantar
if $LIMPIO; then
    paso "Limpieza total (se borran los datos)"
    docker compose down -v --remove-orphans || true
    ok "Volúmenes eliminados"
fi

paso "Construyendo imágenes (la primera vez tarda varios minutos)"
docker compose build
ok "Imágenes listas"

paso "Levantando los 13 contenedores"
docker compose up -d
ok "Contenedores arrancados"

# ---------------------------------------------------------------- 5. Esperar
paso "Esperando a que el sistema esté sano"

esperar() {
    local url=$1 nombre=$2 intentos=${3:-30}
    for i in $(seq 1 "$intentos"); do
        if curl -sf --max-time 2 "$url" > /dev/null 2>&1; then
            ok "$nombre responde"
            return 0
        fi
        sleep 2
    done
    error "$nombre no respondió tras $((intentos * 2))s. Revisa: docker compose logs"
}

esperar "http://localhost:8500/heartbeat" "Middleware"
esperar "http://localhost:8000/heartbeat" "Load balancer"
esperar "http://localhost:8000/alumnos"   "Servicio de alumnos"
esperar "http://localhost:8000/materias"  "Servicio de materias"
esperar "http://localhost:3000"           "Frontend"

# El middleware necesita un ciclo de heartbeat para marcar todo sano.
sleep 6
SANAS=$(curl -s http://localhost:8500/status | grep -oP '"sanas":\s*\K\d+' || echo 0)
if [[ "$SANAS" == "9" ]]; then
    ok "Las 9 instancias están sanas"
else
    aviso "Solo $SANAS de 9 instancias sanas. Espera unos segundos y revisa /status."
fi

# ---------------------------------------------------------------- 6. Resumen
echo -e "\n${VERDE}=============================================${NC}"
echo -e "${VERDE}  Sistema listo${NC}"
echo -e "${VERDE}=============================================${NC}"
echo "  Frontend        http://localhost:3000"
echo "  Load balancer   http://localhost:8000"
echo "  Middleware      http://localhost:8500/status"
echo ""
echo "  Pruebas:  ./demo.sh"
echo "  Apagar:   docker compose down"
echo ""
