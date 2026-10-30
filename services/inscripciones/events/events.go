// Package events define los contratos de los eventos: que topicos existen,
// que consumer groups los leen y que campos lleva cada evento.
//
// Es el mismo archivo en todos los servicios. Tratalo como una API publica:
// si cambias un campo, cambialo igual en todos, porque un evento que ya esta
// en Kafka se va a leer con la version nueva del codigo.
package events

import "time"

// Topicos. El nombre dice quien publica y que paso, en pasado: un evento es
// un hecho que ya ocurrio, no una orden.
const (
	TopicoAlumnosRegistrados      = "alumnos.registrados"      // publica: alumnos
	TopicoInscripcionesCreadas    = "inscripciones.creadas"    // publica: inscripciones
	TopicoInscripcionesCanceladas = "inscripciones.canceladas" // publica: inscripciones
	TopicoGruposLlenos            = "grupos.llenos"            // publica: materias
	TopicoLugaresLiberados        = "lugares.liberados"        // publica: materias
)

// Consumer groups. Cada uno es una cola independiente: cada grupo recibe su
// propia copia de cada evento, y dentro del grupo las instancias se reparten
// el trabajo.
const (
	ConsumidorMaterias       = "materias"       // lee: inscripciones.canceladas
	ConsumidorNotificaciones = "notificaciones" // lee: los cuatro topicos de avisos
	ConsumidorInscripciones  = "inscripciones"  // lee: lugares.liberados (lista de espera)
)

// AlumnoRegistrado: se dio de alta un alumno nuevo.
type AlumnoRegistrado struct {
	EventoID  string    `json:"evento_id"` // permite detectar duplicados
	Matricula string    `json:"matricula"`
	Nombre    string    `json:"nombre"`
	Carrera   string    `json:"carrera"`
	Semestre  int       `json:"semestre"`
	Fecha     time.Time `json:"fecha"`
}

// InscripcionCreada: un alumno quedo inscrito en un grupo. DesdeListaEspera
// dice si entro solo, porque se libero un lugar y era el primero de la fila.
type InscripcionCreada struct {
	EventoID         string    `json:"evento_id"`
	InscripcionID    int       `json:"inscripcion_id"`
	Matricula        string    `json:"matricula"`
	GrupoID          int       `json:"grupo_id"`
	MateriaClave     string    `json:"materia_clave"`
	DesdeListaEspera bool      `json:"desde_lista_espera"`
	Fecha            time.Time `json:"fecha"`
}

// InscripcionCancelada: un alumno se dio de baja de un grupo. El servicio de
// materias lo escucha para devolver el lugar.
type InscripcionCancelada struct {
	EventoID      string    `json:"evento_id"`
	InscripcionID int       `json:"inscripcion_id"`
	Matricula     string    `json:"matricula"`
	GrupoID       int       `json:"grupo_id"`
	MateriaClave  string    `json:"materia_clave"`
	Fecha         time.Time `json:"fecha"`
}

// GrupoLleno: se ocupo el ultimo lugar de un grupo.
type GrupoLleno struct {
	EventoID     string    `json:"evento_id"`
	GrupoID      int       `json:"grupo_id"`
	MateriaClave string    `json:"materia_clave"`
	Numero       string    `json:"numero"`
	CupoMaximo   int       `json:"cupo_maximo"`
	Fecha        time.Time `json:"fecha"`
}

// LugarLiberado: materias YA devolvio un lugar del grupo (lo publica despues
// de bajar el contador, no antes). Inscripciones lo escucha para inscribir al
// primero de la lista de espera.
type LugarLiberado struct {
	EventoID     string    `json:"evento_id"`
	GrupoID      int       `json:"grupo_id"`
	MateriaClave string    `json:"materia_clave"`
	Fecha        time.Time `json:"fecha"`
}
