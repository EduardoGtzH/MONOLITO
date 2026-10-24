package models

import (
	"log"

	"inscripciones.com/inscripciones/connectors"
)

// Proceso orchestrates enrollment across the three services.
type Proceso struct {
	Model     *InscripcionModel
	Connector *connectors.ServiciosConnector
	Eventos   *connectors.EventosConnector
}

// Inscribir runs the validation chain, claims the seat in the materias
// service, then writes the row in this service's own database.
//
// These are two databases, so there is no single transaction covering both.
// If the local insert fails after the seat was claimed, we release the seat
// by hand — a compensating action. This is the trade-off of splitting data
// across services: you give up ACID across the boundary and repair by hand.
func (p *Proceso) Inscribir(matricula string, grupoID int) (*Inscripcion, error) {
	ctx := &Contexto{
		Matricula: matricula,
		GrupoID:   grupoID,
		model:     p.Model,
		connector: p.Connector,
	}

	if err := Validar(ctx); err != nil {
		return nil, err
	}

	if err := p.Connector.ApartarLugar(grupoID); err != nil {
		return nil, err
	}

	inscripcion, err := p.Model.Create(matricula, grupoID, ctx.Grupo.MateriaClave)
	if err != nil {
		log.Printf("insert fallido, liberando lugar del grupo %d: %v", grupoID, err)
		if errLib := p.Connector.LiberarLugar(grupoID); errLib != nil {
			log.Printf("ADVERTENCIA: no se pudo liberar el lugar: %v", errLib)
		}
		return nil, err
	}

	// La inscripcion ya esta guardada. El evento es un aviso para quien le
	// interese: si Kafka falla, la inscripcion no se deshace.
	err = p.Eventos.InscripcionCreada(inscripcion.ID, matricula, grupoID,
		inscripcion.MateriaClave)
	if err != nil {
		log.Printf("ADVERTENCIA: inscripcion %d guardada, pero Kafka no confirmo el evento (puede llegar tarde o perderse): %v",
			inscripcion.ID, err)
	}
	return inscripcion, nil
}

// DarDeBaja marks the row as dropped. El lugar ya no se devuelve aqui con
// una llamada HTTP: se publica inscripciones.canceladas, y materias, que es
// el dueño de los cupos, lo devuelve cuando lee el evento.
//
// Ventaja: si materias esta caido, la baja no falla ni se pierde; el evento
// espera en Kafka y se procesa cuando materias regresa. Costo: por un
// instante el lugar todavia se ve ocupado (consistencia eventual).
func (p *Proceso) DarDeBaja(matricula string, grupoID int) error {
	inscripcion, err := p.Model.Baja(matricula, grupoID)
	if err != nil {
		return err
	}

	err = p.Eventos.InscripcionCancelada(inscripcion.ID, matricula, grupoID,
		inscripcion.MateriaClave)
	if err != nil {
		log.Printf("ADVERTENCIA: baja %d registrada, pero Kafka no confirmo el evento; si se perdio, el lugar no se devuelve: %v",
			inscripcion.ID, err)
	}
	return nil
}
