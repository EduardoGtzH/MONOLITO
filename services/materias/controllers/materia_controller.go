package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"inscripciones.com/materias/models"
)

type MateriaController struct {
	MateriaModel *models.MateriaModel
}

func responderJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// responderError maps the model's sentinel errors to HTTP status codes.
func responderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrGrupoNoEncontrado),
		errors.Is(err, models.ErrMateriaNoEncontrada):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, models.ErrSinCupo):
		// 409 Conflict: the request is valid, the current state rejects it.
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// MateriasHandler serves the catalogue: /materias
func (c *MateriaController) MateriasHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
		return
	}
	materias, err := c.MateriaModel.GetCatalogo()
	if err != nil {
		responderError(w, err)
		return
	}
	responderJSON(w, http.StatusOK, materias)
}

// MateriaHandler serves one course: /materias/TC2027
func (c *MateriaController) MateriaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
		return
	}
	clave := strings.TrimPrefix(r.URL.Path, "/materias/")
	if clave == "" {
		http.Error(w, "falta la clave de la materia", http.StatusBadRequest)
		return
	}
	materia, err := c.MateriaModel.GetByClave(clave)
	if err != nil {
		responderError(w, err)
		return
	}
	responderJSON(w, http.StatusOK, materia)
}

// GrupoHandler serves the /grupos/ subtree, which has three shapes:
//
//	GET  /grupos/3
//	POST /grupos/3/apartar
//	POST /grupos/3/liberar
//
// The mux only matches the prefix, so the id and the action are split out
// of the path by hand.
func (c *MateriaController) GrupoHandler(w http.ResponseWriter, r *http.Request) {
	ruta := strings.Trim(strings.TrimPrefix(r.URL.Path, "/grupos/"), "/")
	if ruta == "" {
		http.Error(w, "falta el id del grupo", http.StatusBadRequest)
		return
	}

	partes := strings.Split(ruta, "/")
	id, err := strconv.Atoi(partes[0])
	if err != nil {
		http.Error(w, "id de grupo invalido", http.StatusBadRequest)
		return
	}

	accion := ""
	if len(partes) > 1 {
		accion = partes[1]
	}

	switch {
	case accion == "" && r.Method == http.MethodGet:
		grupo, err := c.MateriaModel.GetGrupo(id)
		if err != nil {
			responderError(w, err)
			return
		}
		responderJSON(w, http.StatusOK, grupo)

	case accion == "apartar" && r.Method == http.MethodPost:
		grupo, err := c.MateriaModel.ApartarLugar(id)
		if err != nil {
			responderError(w, err)
			return
		}
		responderJSON(w, http.StatusOK, grupo)

	case accion == "liberar" && r.Method == http.MethodPost:
		grupo, err := c.MateriaModel.LiberarLugar(id)
		if err != nil {
			responderError(w, err)
			return
		}
		responderJSON(w, http.StatusOK, grupo)

	default:
		http.Error(w, "ruta o metodo no permitido", http.StatusMethodNotAllowed)
	}
}
