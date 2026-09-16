package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"inscripciones.com/inscripciones/connectors"
	"inscripciones.com/inscripciones/models"
)

type InscripcionController struct {
	Proceso *models.Proceso
}

func responderJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// responderError maps each failure to the HTTP status that describes it:
// 404 the resource does not exist, 409 the rules reject it, 503 a service
// this one depends on is down.
func responderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connectors.ErrAlumnoNoEncontrado),
		errors.Is(err, connectors.ErrGrupoNoEncontrado),
		errors.Is(err, models.ErrInscripcionNoEncontrada):
		http.Error(w, err.Error(), http.StatusNotFound)

	case errors.Is(err, models.ErrAlumnoInactivo),
		errors.Is(err, models.ErrYaInscrito),
		errors.Is(err, models.ErrMateriaRepetida),
		errors.Is(err, models.ErrEmpalme),
		errors.Is(err, models.ErrSinCupo):
		http.Error(w, err.Error(), http.StatusConflict)

	case errors.Is(err, connectors.ErrServicioCaido):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)

	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type peticionInscripcion struct {
	Matricula string `json:"matricula"`
	GrupoID   int    `json:"grupo_id"`
}

// InscripcionesHandler serves the collection: /inscripciones
//
//	POST /inscripciones            -> enroll
//	GET  /inscripciones?matricula= -> list a student's enrollments
func (c *InscripcionController) InscripcionesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		matricula := r.URL.Query().Get("matricula")
		if matricula == "" {
			http.Error(w, "falta el parametro 'matricula'", http.StatusBadRequest)
			return
		}
		lista, err := c.Proceso.Model.GetByMatricula(matricula)
		if err != nil {
			responderError(w, err)
			return
		}
		responderJSON(w, http.StatusOK, lista)

	case http.MethodPost:
		var p peticionInscripcion
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "cuerpo invalido", http.StatusBadRequest)
			return
		}
		if p.Matricula == "" || p.GrupoID == 0 {
			http.Error(w, "matricula y grupo_id son obligatorios", http.StatusBadRequest)
			return
		}
		inscripcion, err := c.Proceso.Inscribir(p.Matricula, p.GrupoID)
		if err != nil {
			responderError(w, err)
			return
		}
		responderJSON(w, http.StatusCreated, inscripcion)

	default:
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
	}
}

// BajaHandler drops an enrollment: DELETE /inscripciones/A01234567/3
func (c *InscripcionController) BajaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
		return
	}

	ruta := strings.Trim(strings.TrimPrefix(r.URL.Path, "/inscripciones/"), "/")
	partes := strings.Split(ruta, "/")
	if len(partes) != 2 {
		http.Error(w, "usa /inscripciones/{matricula}/{grupo_id}", http.StatusBadRequest)
		return
	}

	grupoID, err := strconv.Atoi(partes[1])
	if err != nil {
		http.Error(w, "id de grupo invalido", http.StatusBadRequest)
		return
	}

	if err := c.Proceso.DarDeBaja(partes[0], grupoID); err != nil {
		responderError(w, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]string{"estado": "dada de baja"})
}
