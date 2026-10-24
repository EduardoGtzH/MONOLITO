#!/usr/bin/env bash
#
# crear-topicos.sh — declara los topicos del sistema.
#
# Lo ejecuta el contenedor kafka-init en cuanto Kafka esta sano, y termina.
# Cada servicio es dueño de los topicos donde publica sus propios hechos:
#
#   topico                     lo publica      lo consumen (grupos)
#   alumnos.registrados        alumnos         notificaciones
#   inscripciones.creadas      inscripciones   notificaciones
#   inscripciones.canceladas   inscripciones   materias, notificaciones
#   grupos.llenos              materias        notificaciones
#
# 3 particiones: hasta 3 instancias de un mismo grupo leyendo en paralelo,
# una por replica de cada servicio. Una cuarta instancia se quedaria sin
# particion y esperaria de reserva.
#
# --if-not-exists hace que correrlo dos veces no falle: kafka-init vuelve a
# ejecutarse cada vez que se levanta el sistema.
set -euo pipefail

BROKER="${KAFKA_BROKER:-kafka:9092}"
TOPICOS=(
    alumnos.registrados
    inscripciones.creadas
    inscripciones.canceladas
    grupos.llenos
)

for topico in "${TOPICOS[@]}"; do
    /opt/kafka/bin/kafka-topics.sh --bootstrap-server "$BROKER" \
        --create --if-not-exists --topic "$topico" \
        --partitions 3 --replication-factor 1
done

echo "Topicos listos:"
/opt/kafka/bin/kafka-topics.sh --bootstrap-server "$BROKER" --describe
