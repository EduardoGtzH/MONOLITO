package connectors

import (
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"

	"inscripciones.com/inscripciones/events"
	"inscripciones.com/inscripciones/kafkax"
)

// EventosConnector publica en Kafka los hechos de este servicio. Vive junto a
// ServiciosConnector porque los dos hacen lo mismo: hablar con otro sistema.
// La diferencia es que aqui nadie contesta: se publica y se sigue.
type EventosConnector struct {
	creadas    *kafka.Writer
	canceladas *kafka.Writer
}

func NewEventosConnector(brokers []string) *EventosConnector {
	return &EventosConnector{
		creadas:    kafkax.NewWriter(brokers, events.TopicoInscripcionesCreadas),
		canceladas: kafkax.NewWriter(brokers, events.TopicoInscripcionesCanceladas),
	}
}

// InscripcionCreada publica en inscripciones.creadas. La clave es la
// matricula: los eventos de un mismo alumno caen en la misma particion y se
// leen en orden.
func (c *EventosConnector) InscripcionCreada(id int, matricula string, grupoID int, clave string) error {
	evento := events.InscripcionCreada{
		EventoID:      uuid.NewString(),
		InscripcionID: id,
		Matricula:     matricula,
		GrupoID:       grupoID,
		MateriaClave:  clave,
		Fecha:         time.Now().UTC(),
	}
	return kafkax.Publish(c.creadas, matricula, evento)
}

// InscripcionCancelada publica en inscripciones.canceladas.
func (c *EventosConnector) InscripcionCancelada(id int, matricula string, grupoID int, clave string) error {
	evento := events.InscripcionCancelada{
		EventoID:      uuid.NewString(),
		InscripcionID: id,
		Matricula:     matricula,
		GrupoID:       grupoID,
		MateriaClave:  clave,
		Fecha:         time.Now().UTC(),
	}
	return kafkax.Publish(c.canceladas, matricula, evento)
}

// Close manda lo que quede pendiente. Se llama al apagar el servicio.
func (c *EventosConnector) Close() {
	c.creadas.Close()
	c.canceladas.Close()
}
