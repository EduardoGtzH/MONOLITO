package models

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"inscripciones.com/inscripciones/connectors"
)

var (
	ErrAlumnoInactivo  = errors.New("el alumno esta dado de baja")
	ErrYaInscrito      = errors.New("el alumno ya esta inscrito en este grupo")
	ErrMateriaRepetida = errors.New("el alumno ya lleva otro grupo de esta materia")
	ErrEmpalme         = errors.New("el horario se empalma con otra materia inscrita")
	ErrSinCupo         = connectors.ErrSinCupo

	ErrFueraDePeriodo = errors.New("el periodo de inscripcion esta cerrado")
	ErrYaAprobada     = errors.New("el alumno ya aprobo esta materia")
	ErrPrerrequisito  = errors.New("falta aprobar un prerrequisito")
	ErrCreditos       = errors.New("se pasa del limite de creditos del semestre")
	ErrHayFila        = errors.New("hay alumnos en lista de espera para este grupo")
	ErrGrupoConLugar  = errors.New("el grupo todavia tiene lugares: inscribete directamente")
)

// Reglas son los parametros de la cadena que no salen de ningun servicio:
// vienen del entorno (docker-compose.yml). Inicio o Fin en cero significa
// "sin limite" de ese lado.
type Reglas struct {
	MaxCreditos int
	Inicio      time.Time
	Fin         time.Time
}

// Contexto carries everything the validations need. Each link fills in the
// data it fetched so later links can reuse it instead of calling again.
type Contexto struct {
	Matricula string
	GrupoID   int

	Alumno *connectors.AlumnoRemoto
	Grupo  *connectors.GrupoRemoto

	Actuales []Inscripcion                   // rows from this service's own DB
	Horarios map[int]*connectors.GrupoRemoto // grupo id -> schedule, fetched remotely

	model     *InscripcionModel
	connector *connectors.ServiciosConnector
	fila      *FilaModel
	reglas    Reglas
}

// Validacion is one link in the chain. Returning an error stops the chain.
type Validacion func(ctx *Contexto) error

// cadena is the ordered list of rules. Adding a rule is adding a line here:
// no existing function changes. Cheap local checks run before expensive
// remote ones so an obviously invalid request fails fast.
//
// Las reglas nuevas (periodo, materia ya aprobada, prerrequisitos, creditos y
// lista de espera) entraron asi: una funcion y una linea cada una.
var cadena = []Validacion{
	validarPeriodo,
	validarAlumnoExiste,
	validarGrupoExiste,
	validarNoAprobada,
	validarPrerrequisitos,
	validarNoInscritoYa,
	validarMateriaNoRepetida,
	validarSinEmpalme,
	validarLimiteCreditos,
	validarSinFila,
	validarCupoDisponible,
}

// cadenaFila es para formarse en la lista de espera. Son las mismas reglas,
// porque formarse es pedir "inscribeme en cuanto haya lugar", salvo las dos
// ultimas: aqui se pide justo lo contrario, que el grupo NO tenga lugar.
var cadenaFila = []Validacion{
	validarPeriodo,
	validarAlumnoExiste,
	validarGrupoExiste,
	validarNoAprobada,
	validarPrerrequisitos,
	validarNoInscritoYa,
	validarMateriaNoRepetida,
	validarSinEmpalme,
	validarLimiteCreditos,
	validarGrupoSinLugar,
}

// Validar runs the chain and returns the first failure.
func Validar(ctx *Contexto) error {
	return correr(cadena, ctx)
}

// ValidarFila corre la cadena para formarse en la lista de espera.
func ValidarFila(ctx *Contexto) error {
	return correr(cadenaFila, ctx)
}

func correr(reglas []Validacion, ctx *Contexto) error {
	for _, validacion := range reglas {
		if err := validacion(ctx); err != nil {
			return err
		}
	}
	return nil
}

// validarPeriodo va primero porque no consulta nada: solo la hora.
func validarPeriodo(ctx *Contexto) error {
	ahora := time.Now()
	if !ctx.reglas.Inicio.IsZero() && ahora.Before(ctx.reglas.Inicio) {
		return fmt.Errorf("%w: abre el %s", ErrFueraDePeriodo,
			ctx.reglas.Inicio.Format("2006-01-02 15:04"))
	}
	if !ctx.reglas.Fin.IsZero() && ahora.After(ctx.reglas.Fin) {
		return fmt.Errorf("%w: cerro el %s", ErrFueraDePeriodo,
			ctx.reglas.Fin.Format("2006-01-02 15:04"))
	}
	return nil
}

// validarNoAprobada: no tiene sentido inscribir una materia que ya se aprobo.
// Las aprobadas las trae el servicio de alumnos, que es el dueño del
// historial academico.
func validarNoAprobada(ctx *Contexto) error {
	for _, c := range ctx.Alumno.Aprobadas {
		if c == ctx.Grupo.MateriaClave {
			return fmt.Errorf("%w (%s)", ErrYaAprobada, c)
		}
	}
	return nil
}

// validarPrerrequisitos cruza lo que pide la materia (dato de materias) con
// lo que aprobo el alumno (dato de alumnos). Ningun servicio solo puede
// hacer esta validacion: por eso vive en inscripciones, que habla con ambos.
func validarPrerrequisitos(ctx *Contexto) error {
	aprobadas := make(map[string]bool, len(ctx.Alumno.Aprobadas))
	for _, c := range ctx.Alumno.Aprobadas {
		aprobadas[c] = true
	}
	faltan := make([]string, 0)
	for _, r := range ctx.Grupo.Requiere {
		if !aprobadas[r] {
			faltan = append(faltan, r)
		}
	}
	if len(faltan) > 0 {
		return fmt.Errorf("%w: %s requiere %s", ErrPrerrequisito,
			ctx.Grupo.MateriaClave, strings.Join(faltan, ", "))
	}
	return nil
}

// validarLimiteCreditos suma los creditos de lo que ya lleva mas lo nuevo.
// Va despues de validarSinEmpalme porque reutiliza los grupos que esa
// validacion ya trajo de materias: no hace ni una llamada extra.
func validarLimiteCreditos(ctx *Contexto) error {
	if ctx.reglas.MaxCreditos <= 0 {
		return nil
	}
	total := ctx.Grupo.Creditos
	for _, g := range ctx.Horarios {
		total += g.Creditos
	}
	if total > ctx.reglas.MaxCreditos {
		return fmt.Errorf("%w: serian %d de %d", ErrCreditos, total, ctx.reglas.MaxCreditos)
	}
	return nil
}

// validarSinFila: si hay alumnos esperando un lugar en este grupo, nadie se
// les puede adelantar inscribiendose directo. La unica excepcion es el
// primero de la fila, que es justo a quien le toca el lugar.
func validarSinFila(ctx *Contexto) error {
	total, primero, err := ctx.fila.Esperando(ctx.GrupoID)
	if err != nil {
		return err
	}
	if total > 0 && primero != ctx.Matricula {
		return fmt.Errorf("%w (%d esperando): formate en la fila", ErrHayFila, total)
	}
	return nil
}

// validarGrupoSinLugar es la condicion para formarse: el grupo esta lleno,
// o ya hay fila (un lugar recien liberado le toca al primero de la fila).
func validarGrupoSinLugar(ctx *Contexto) error {
	if ctx.Grupo.CupoOcupado < ctx.Grupo.CupoMaximo {
		total, _, err := ctx.fila.Esperando(ctx.GrupoID)
		if err != nil {
			return err
		}
		if total == 0 {
			return ErrGrupoConLugar
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
			return fmt.Errorf("%w (%s, ya inscrito en el grupo %d)", ErrMateriaRepetida,
				i.MateriaClave, i.GrupoID)
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
