package models

import (
	"log"

	"inscripciones.com/inscripciones/connectors"
)

// Proceso orchestrates enrollment across the three services.
type Proceso struct {
	Model     *InscripcionModel
	Connector *connectors.ServiciosConnector
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
	return inscripcion, nil
}

// DarDeBaja marks the row as dropped and hands the seat back.
func (p *Proceso) DarDeBaja(matricula string, grupoID int) error {
	if err := p.Model.Baja(matricula, grupoID); err != nil {
		return err
	}
	if err := p.Connector.LiberarLugar(grupoID); err != nil {
		log.Printf("ADVERTENCIA: baja registrada pero no se libero el lugar: %v", err)
	}
	return nil
}
