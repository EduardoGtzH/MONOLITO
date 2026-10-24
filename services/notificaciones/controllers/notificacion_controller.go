package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/segmentio/kafka-go"

	"inscripciones.com/notificaciones/events"
)

// NotificacionController es la C de MVC, pero lo que le llega no es una
// peticion HTTP sino un mensaje de Kafka. No hay modelo porque este servicio
// no tiene base de datos: solo "manda correos" (aqui, los escribe en el log).
type NotificacionController struct {
	Instancia string

	mu     sync.Mutex      // las 4 goroutines (una por topico) usan el mapa
	vistos map[string]bool // evento_id que ya se notificaron
}

func NewNotificacionController(instancia string) *NotificacionController {
	return &NotificacionController{Instancia: instancia, vistos: map[string]bool{}}
}

// primeraVez evita mandar dos veces el mismo correo cuando Kafka reentrega
// un evento. Es memoria de ESTA instancia: si el evento se reentrega a la
// otra, esa no lo conoce. Para un correo es aceptable; para devolver un lugar
// no lo es, y por eso materias guarda los eventos procesados en su base.
func (c *NotificacionController) primeraVez(eventoID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.vistos[eventoID] {
		return false
	}
	c.vistos[eventoID] = true
	return true
}

// Manejar recibe los eventos de los cuatro topicos. msg.Topic dice de cual
// vino, y con eso se sabe que estructura tiene el JSON.
func (c *NotificacionController) Manejar(_ context.Context, msg kafka.Message) error {
	var eventoID, correo string

	switch msg.Topic {
	case events.TopicoAlumnosRegistrados:
		var e events.AlumnoRegistrado
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			return c.ilegible(msg, err)
		}
		eventoID = e.EventoID
		correo = fmt.Sprintf("bienvenida a %s (%s), alumno de %s", e.Nombre, e.Matricula, e.Carrera)

	case events.TopicoInscripcionesCreadas:
		var e events.InscripcionCreada
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			return c.ilegible(msg, err)
		}
		eventoID = e.EventoID
		correo = fmt.Sprintf("%s quedo inscrito en %s (grupo %d)", e.Matricula, e.MateriaClave, e.GrupoID)

	case events.TopicoInscripcionesCanceladas:
		var e events.InscripcionCancelada
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			return c.ilegible(msg, err)
		}
		eventoID = e.EventoID
		correo = fmt.Sprintf("%s se dio de baja de %s (grupo %d)", e.Matricula, e.MateriaClave, e.GrupoID)

	case events.TopicoGruposLlenos:
		var e events.GrupoLleno
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			return c.ilegible(msg, err)
		}
		eventoID = e.EventoID
		correo = fmt.Sprintf("a coordinacion: el grupo %s-%s ya ocupo sus %d lugares",
			e.MateriaClave, e.Numero, e.CupoMaximo)

	default:
		return nil
	}

	if !c.primeraVez(eventoID) {
		log.Printf("[%s] evento %s repetido, no se manda otra vez", c.Instancia, eventoID)
		return nil
	}
	log.Printf("[%s] %s particion=%d offset=%d -> correo: %s",
		c.Instancia, msg.Topic, msg.Partition, msg.Offset, correo)
	return nil
}

// ilegible: un mensaje que no es JSON valido nunca va a funcionar, por mas
// que se reintente. Se registra y se devuelve nil para seguir con el resto.
func (c *NotificacionController) ilegible(msg kafka.Message, err error) error {
	log.Printf("[%s] mensaje ilegible en %s offset %d, se ignora: %v",
		c.Instancia, msg.Topic, msg.Offset, err)
	return nil
}
