package connectors

import (
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"

	"inscripciones.com/materias/events"
	"inscripciones.com/materias/kafkax"
)

// EventosConnector publica en Kafka los hechos de este servicio.
type EventosConnector struct {
	llenos *kafka.Writer
}

func NewEventosConnector(brokers []string) *EventosConnector {
	return &EventosConnector{
		llenos: kafkax.NewWriter(brokers, events.TopicoGruposLlenos),
	}
}

// GrupoLleno publica en grupos.llenos. La clave es el id del grupo.
func (c *EventosConnector) GrupoLleno(grupoID int, clave, numero string, cupoMaximo int) error {
	evento := events.GrupoLleno{
		EventoID:     uuid.NewString(),
		GrupoID:      grupoID,
		MateriaClave: clave,
		Numero:       numero,
		CupoMaximo:   cupoMaximo,
		Fecha:        time.Now().UTC(),
	}
	return kafkax.Publish(c.llenos, strconv.Itoa(grupoID), evento)
}

// Close manda lo que quede pendiente. Se llama al apagar el servicio.
func (c *EventosConnector) Close() error {
	return c.llenos.Close()
}
