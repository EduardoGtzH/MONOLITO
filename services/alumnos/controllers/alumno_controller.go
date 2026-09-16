package controllers

import (
	"encoding/json"
	"net/http"
	"strings"

	"inscripciones.com/alumnos/models"
)

// AlumnoController is the C in MVC: it parses the HTTP request, delegates to
// the model, and writes the JSON response. No business logic, no SQL.
type AlumnoController struct {
	AlumnoModel *models.AlumnoModel
}

// responderJSON is the "view" of this service: in an API the rendered view
// is the JSON representation.
func responderJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// AlumnosHandler serves the collection: /alumnos
func (c *AlumnoController) AlumnosHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		alumnos, err := c.AlumnoModel.GetAll()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		responderJSON(w, http.StatusOK, alumnos)

	case http.MethodPost:
		var nuevo models.Alumno
		if err := json.NewDecoder(r.Body).Decode(&nuevo); err != nil {
			http.Error(w, "cuerpo invalido", http.StatusBadRequest)
			return
		}
		if nuevo.Matricula == "" || nuevo.Nombre == "" {
			http.Error(w, "matricula y nombre son obligatorios", http.StatusBadRequest)
			return
		}
		if nuevo.Semestre < 1 || nuevo.Semestre > 12 {
			http.Error(w, "semestre debe estar entre 1 y 12", http.StatusBadRequest)
			return
		}

		alumno, err := c.AlumnoModel.Create(nuevo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		responderJSON(w, http.StatusCreated, alumno)

	default:
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
	}
}

// AlumnoHandler serves a single resource: /alumnos/A01234567
// The trailing slash in the mux pattern makes this a subtree match, so we
// pull the matricula out of the path by hand.
func (c *AlumnoController) AlumnoHandler(w http.ResponseWriter, r *http.Request) {
	matricula := strings.TrimPrefix(r.URL.Path, "/alumnos/")
	if matricula == "" {
		http.Error(w, "falta la matricula", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		alumno, err := c.AlumnoModel.GetByMatricula(matricula)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		responderJSON(w, http.StatusOK, alumno)

	default:
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
	}
}
