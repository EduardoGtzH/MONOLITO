package controllers

import (
	"context"
	"encoding/json"
	"log"

	"github.com/segmentio/kafka-go"

	"inscripciones.com/inscripciones/events"
	"inscripciones.com/inscripciones/models"
)

// EventosController es la entrada por Kafka de este servicio, igual que en
// materias: recibe un evento y llama al mismo Proceso que usa la API HTTP.
type EventosController struct {
	Proceso *models.Proceso
}

// LugarLiberado: materias ya devolvio un lugar del grupo. Le toca al primero
// de la lista de espera. Si regresa error, kafkax lo reintenta.
func (c *EventosController) LugarLiberado(_ context.Context, msg kafka.Message) error {
	var e events.LugarLiberado
	if err := json.Unmarshal(msg.Value, &e); err != nil {
		log.Printf("[fila] mensaje ilegible en offset %d, se ignora: %v", msg.Offset, err)
		return nil
	}
	return c.Proceso.AtenderFila(e.GrupoID)
}
