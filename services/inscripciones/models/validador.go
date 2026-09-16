package models

import (
	"errors"
	"fmt"
	"sync"

	"inscripciones.com/inscripciones/connectors"
)

var (
	ErrAlumnoInactivo = errors.New("el alumno esta dado de baja")
	ErrYaInscrito     = errors.New("el alumno ya esta inscrito en este grupo")
	ErrMateriaRepetida = errors.New("el alumno ya lleva otro grupo de esta materia")
	ErrEmpalme        = errors.New("el horario se empalma con otra materia inscrita")
	ErrSinCupo        = connectors.ErrSinCupo
)

// Contexto carries everything the validations need. Each link fills in the
// data it fetched so later links can reuse it instead of calling again.
type Contexto struct {
	Matricula string
	GrupoID   int

	Alumno *connectors.AlumnoRemoto
	Grupo  *connectors.GrupoRemoto

	Actuales []Inscripcion                  // rows from this service's own DB
	Horarios map[int]*connectors.GrupoRemoto // grupo id -> schedule, fetched remotely

	model     *InscripcionModel
	connector *connectors.ServiciosConnector
}

// Validacion is one link in the chain. Returning an error stops the chain.
type Validacion func(ctx *Contexto) error

// cadena is the ordered list of rules. Adding a rule is adding a line here:
// no existing function changes. Cheap local checks run before expensive
// remote ones so an obviously invalid request fails fast.
var cadena = []Validacion{
	validarAlumnoExiste,
	validarGrupoExiste,
	validarNoInscritoYa,
	validarMateriaNoRepetida,
	validarSinEmpalme,
	validarCupoDisponible,
}

// Validar runs the chain and returns the first failure.
func Validar(ctx *Contexto) error {
	for _, validacion := range cadena {
		if err := validacion(ctx); err != nil {
			return err
		}
	}
	return nil
}

func validarAlumnoExiste(ctx *Contexto) error {
	alumno, err := ctx.connector.GetAlumno(ctx.Matricula)
	if err != nil {
		return err
	}
	if !alumno.Activo {
		return ErrAlumnoInactivo
	}
	ctx.Alumno = alumno
	return nil
}

func validarGrupoExiste(ctx *Contexto) error {
	grupo, err := ctx.connector.GetGrupo(ctx.GrupoID)
	if err != nil {
		return err
	}
	ctx.Grupo = grupo
	return nil
}

func validarNoInscritoYa(ctx *Contexto) error {
	actuales, err := ctx.model.GetByMatricula(ctx.Matricula)
	if err != nil {
		return err
	}
	ctx.Actuales = actuales

	for _, i := range actuales {
		if i.GrupoID == ctx.GrupoID {
			return ErrYaInscrito
		}
	}
	return nil
}

func validarMateriaNoRepetida(ctx *Contexto) error {
	for _, i := range ctx.Actuales {
		if i.MateriaClave == ctx.Grupo.MateriaClave {
			return fmt.Errorf("%w (%s grupo %s)", ErrMateriaRepetida,
				i.MateriaClave, ctx.Grupo.Numero)
		}
	}
	return nil
}

// validarSinEmpalme needs the schedule of every group the student is already
// in. Those live in the materias service, so this is N remote calls. They are
// independent, so we fan them out with one goroutine each and join with a
// WaitGroup: total latency is one round trip instead of N.
func validarSinEmpalme(ctx *Contexto) error {
	ctx.Horarios = make(map[int]*connectors.GrupoRemoto)

	var mu sync.Mutex // guards the map: goroutines write to it concurrently
	var wg sync.WaitGroup
	errores := make([]error, len(ctx.Actuales))

	for idx, inscrita := range ctx.Actuales {
		wg.Add(1)
		go func(idx int, grupoID int) {
			defer wg.Done()
			grupo, err := ctx.connector.GetGrupo(grupoID)
			if err != nil {
				errores[idx] = err
				return
			}
			mu.Lock()
			ctx.Horarios[grupoID] = grupo
			mu.Unlock()
		}(idx, inscrita.GrupoID)
	}
	wg.Wait()

	for _, err := range errores {
		if err != nil {
			return err
		}
	}

	for _, otro := range ctx.Horarios {
		if seEmpalman(ctx.Grupo, otro) {
			return fmt.Errorf("%w: %s %s %s-%s", ErrEmpalme,
				otro.MateriaClave, otro.Dia, otro.HoraInicio, otro.HoraFin)
		}
	}
	return nil
}

// seEmpalman compares two sessions. Times arrive as "HH:MM" strings, which
// sort correctly with plain string comparison, so no time parsing is needed.
// Two ranges overlap when each one starts before the other ends.
func seEmpalman(a, b *connectors.GrupoRemoto) bool {
	if a.Dia != b.Dia {
		return false
	}
	return a.HoraInicio < b.HoraFin && b.HoraInicio < a.HoraFin
}

// validarCupoDisponible is an early check for a nicer error message. The
// real guarantee is the FOR UPDATE transaction inside the materias service:
// between this read and the actual claim, another request could take the
// last seat. That claim can still fail, and the controller handles it.
func validarCupoDisponible(ctx *Contexto) error {
	if ctx.Grupo.CupoOcupado >= ctx.Grupo.CupoMaximo {
		return ErrSinCupo
	}
	return nil
}
