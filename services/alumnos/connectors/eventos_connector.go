package connectors

import (
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"

	"inscripciones.com/alumnos/events"
	"inscripciones.com/alumnos/kafkax"
)

// EventosConnector publica en Kafka lo que pasa en este servicio. Es un
// connector igual que el de inscripciones: todo lo que sale del servicio
// hacia otro sistema vive en esta carpeta.
type EventosConnector struct {
	registrados *kafka.Writer
}

func NewEventosConnector(brokers []string) *EventosConnector {
	return &EventosConnector{
		registrados: kafkax.NewWriter(brokers, events.TopicoAlumnosRegistrados),
	}
}

// AlumnoRegistrado publica en alumnos.registrados. La clave es la matricula.
func (c *EventosConnector) AlumnoRegistrado(matricula, nombre, carrera string, semestre int) error {
	evento := events.AlumnoRegistrado{
		EventoID:  uuid.NewString(),
		Matricula: matricula,
		Nombre:    nombre,
		Carrera:   carrera,
		Semestre:  semestre,
		Fecha:     time.Now().UTC(),
	}
	return kafkax.Publish(c.registrados, matricula, evento)
}

// Close manda lo que quede pendiente. Se llama al apagar el servicio.
func (c *EventosConnector) Close() error {
	return c.registrados.Close()
}
