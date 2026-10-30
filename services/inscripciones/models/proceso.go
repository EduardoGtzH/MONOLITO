package models

import (
	"errors"
	"log"

	"inscripciones.com/inscripciones/connectors"
)

// Proceso orchestrates enrollment across the three services.
type Proceso struct {
	Model     *InscripcionModel
	Fila      *FilaModel
	Connector *connectors.ServiciosConnector
	Eventos   *connectors.EventosConnector
	Reglas    Reglas
}

func (p *Proceso) contexto(matricula string, grupoID int) *Contexto {
	return &Contexto{
		Matricula: matricula,
		GrupoID:   grupoID,
		model:     p.Model,
		connector: p.Connector,
		fila:      p.Fila,
		reglas:    p.Reglas,
	}
}

// Inscribir es la inscripcion que pide el alumno (POST /inscripciones).
func (p *Proceso) Inscribir(matricula string, grupoID int) (*Inscripcion, error) {
	return p.inscribir(matricula, grupoID, false)
}

// inscribir runs the validation chain, claims the seat in the materias
// service, then writes the row in this service's own database.
//
// These are two databases, so there is no single transaction covering both.
// If the local insert fails after the seat was claimed, we release the seat
// by hand — a compensating action. This is the trade-off of splitting data
// across services: you give up ACID across the boundary and repair by hand.
//
// desdeLista es true cuando quien inscribe no es el alumno sino la lista de
// espera, al liberarse un lugar (ver AtenderFila).
func (p *Proceso) inscribir(matricula string, grupoID int, desdeLista bool) (*Inscripcion, error) {
	ctx := p.contexto(matricula, grupoID)

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

	// Si estaba formado en la fila de este grupo, ya no espera.
	if err := p.Fila.MarcarInscrito(matricula, grupoID); err != nil {
		log.Printf("ADVERTENCIA: inscripcion %d guardada, pero no se cerro su lugar en la fila: %v",
			inscripcion.ID, err)
	}

	// La inscripcion ya esta guardada. El evento es un aviso para quien le
	// interese: si Kafka falla, la inscripcion no se deshace.
	err = p.Eventos.InscripcionCreada(inscripcion.ID, matricula, grupoID,
		inscripcion.MateriaClave, desdeLista)
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

// Formar pone al alumno en la lista de espera de un grupo lleno. Pasa por
// casi la misma cadena que inscribir: si no podria inscribirse (empalme,
// prerrequisito...), tampoco tiene sentido que espere un lugar.
func (p *Proceso) Formar(matricula string, grupoID int) (*Lugar, error) {
	ctx := p.contexto(matricula, grupoID)
	if err := ValidarFila(ctx); err != nil {
		return nil, err
	}
	return p.Fila.Formar(matricula, grupoID, ctx.Grupo.MateriaClave)
}

// AtenderFila se llama cuando materias avisa que se libero un lugar en el
// grupo (evento lugares.liberados). Inscribe al primero de la fila; si a ese
// ya no le aplica (por ejemplo, mientras esperaba se inscribio en otro grupo
// que ahora le empalma), lo saca de la fila con el motivo y sigue con el
// siguiente.
//
// Sigue mientras haya fila y lugar: se detiene cuando materias dice que ya
// no hay cupo. Por eso un evento repetido no hace dano: el segundo intento
// se encuentra con que el lugar ya se ocupo y no inscribe a nadie de mas.
//
// Regresar error significa "algo de infraestructura fallo, reintentalo"
// (por ejemplo, materias no contesto): kafkax vuelve a entregar el evento.
func (p *Proceso) AtenderFila(grupoID int) error {
	for {
		primero, err := p.Fila.Primero(grupoID)
		if err != nil {
			return err
		}
		if primero == nil {
			return nil // nadie esperando
		}

		_, err = p.inscribir(primero.Matricula, grupoID, true)
		switch {
		case err == nil:
			log.Printf("[fila] %s entro al grupo %d desde la lista de espera",
				primero.Matricula, grupoID)
			// Puede haber mas lugares libres: se intenta con el siguiente.

		case errors.Is(err, ErrSinCupo):
			return nil // ya no hay lugar: el resto sigue esperando

		case errors.Is(err, ErrYaInscrito):
			// Ya habia entrado (un intento anterior se corto a la mitad).
			if err := p.Fila.Resolver(primero.ID, "INSCRITO", "ya estaba inscrito"); err != nil {
				return err
			}

		case errors.Is(err, ErrFueraDePeriodo):
			log.Printf("[fila] periodo cerrado: el grupo %d no promueve a nadie", grupoID)
			return nil

		case errors.Is(err, connectors.ErrServicioCaido):
			return err // materias o alumnos no contesto: que se reintente

		case esReglaDeNegocio(err):
			log.Printf("[fila] %s sale de la fila del grupo %d: %v", primero.Matricula, grupoID, err)
			if err := p.Fila.Resolver(primero.ID, "CANCELADA", err.Error()); err != nil {
				return err
			}

		default:
			return err
		}
	}
}

// esReglaDeNegocio: errores que no se arreglan reintentando, porque son del
// alumno y no de la infraestructura.
func esReglaDeNegocio(err error) bool {
	for _, e := range []error{ErrAlumnoInactivo, ErrMateriaRepetida, ErrEmpalme,
		ErrYaAprobada, ErrPrerrequisito, ErrCreditos, ErrHayFila,
		connectors.ErrAlumnoNoEncontrado, connectors.ErrGrupoNoEncontrado} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
