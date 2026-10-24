package controllers

import (
	"context"
	"encoding/json"
	"log"

	"github.com/segmentio/kafka-go"

	"inscripciones.com/materias/events"
	"inscripciones.com/materias/models"
)

// EventosController es la C de MVC para los mensajes de Kafka. Igual que
// MateriaController recibe una peticion HTTP y llama al modelo, este recibe
// un evento y llama al mismo modelo.
type EventosController struct {
	MateriaModel *models.MateriaModel
}

// InscripcionCancelada devuelve el lugar del grupo. Si regresa error, kafkax
// lo reintenta; si regresa nil, se confirma el offset y sigue el siguiente.
func (c *EventosController) InscripcionCancelada(_ context.Context, msg kafka.Message) error {
	var e events.InscripcionCancelada
	if err := json.Unmarshal(msg.Value, &e); err != nil {
		// Reintentar un JSON roto nunca va a funcionar: se registra y se sigue.
		log.Printf("[materias] mensaje ilegible en offset %d, se ignora: %v", msg.Offset, err)
		return nil
	}

	liberado, err := c.MateriaModel.LiberarPorEvento(e.EventoID, e.GrupoID)
	if err != nil {
		return err
	}
	if !liberado {
		log.Printf("[materias] evento %s repetido: el lugar ya se habia devuelto", e.EventoID)
		return nil
	}
	log.Printf("[materias] lugar devuelto al grupo %d por la baja de %s (particion=%d offset=%d)",
		e.GrupoID, e.Matricula, msg.Partition, msg.Offset)
	return nil
}
