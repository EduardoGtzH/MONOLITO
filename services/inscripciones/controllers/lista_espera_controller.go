package controllers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"inscripciones.com/inscripciones/models"
)

// ListaEsperaHandler sirve la coleccion /lista-espera
//
//	POST /lista-espera            -> formarse. Cuerpo: {"matricula":"...","grupo_id":N}
//	GET  /lista-espera?matricula= -> donde esta formado un alumno, y su historial
//	GET  /lista-espera?grupo_id=  -> la fila actual de un grupo, en orden
func (c *InscripcionController) ListaEsperaHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		var lista []models.Lugar
		var err error
		switch {
		case q.Get("matricula") != "":
			lista, err = c.Proceso.Fila.DeAlumno(q.Get("matricula"))
		case q.Get("grupo_id") != "":
			id, errID := strconv.Atoi(q.Get("grupo_id"))
			if errID != nil {
				http.Error(w, "grupo_id invalido", http.StatusBadRequest)
				return
			}
			lista, err = c.Proceso.Fila.DeGrupo(id)
		default:
			http.Error(w, "usa ?matricula= o ?grupo_id=", http.StatusBadRequest)
			return
		}
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
		lugar, err := c.Proceso.Formar(p.Matricula, p.GrupoID)
		if err != nil {
			responderError(w, err)
			return
		}
		responderJSON(w, http.StatusCreated, lugar)

	default:
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
	}
}

// SalirDeFilaHandler: DELETE /lista-espera/A01234567/4
func (c *InscripcionController) SalirDeFilaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
		return
	}
	partes := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/lista-espera/"), "/"), "/")
	if len(partes) != 2 {
		http.Error(w, "usa /lista-espera/{matricula}/{grupo_id}", http.StatusBadRequest)
		return
	}
	grupoID, err := strconv.Atoi(partes[1])
	if err != nil {
		http.Error(w, "id de grupo invalido", http.StatusBadRequest)
		return
	}
	if err := c.Proceso.Fila.Salir(partes[0], grupoID); err != nil {
		responderError(w, err)
		return
	}
	responderJSON(w, http.StatusOK, map[string]string{"estado": "salio de la fila"})
}
