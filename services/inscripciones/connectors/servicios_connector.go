package connectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	ErrAlumnoNoEncontrado = errors.New("el alumno no existe")
	ErrGrupoNoEncontrado  = errors.New("el grupo no existe")
	ErrSinCupo            = errors.New("el grupo ya no tiene cupo disponible")
	ErrServicioCaido      = errors.New("un servicio remoto no respondio")
)

// AlumnoRemoto mirrors only the fields this service needs from the alumnos
// service. Deliberately partial: we are a consumer of that API, not its owner.
type AlumnoRemoto struct {
	Matricula string `json:"matricula"`
	Nombre    string `json:"nombre"`
	Carrera   string `json:"carrera"`
	Activo    bool   `json:"activo"`
}

// GrupoRemoto mirrors the fields needed to validate schedule and seats.
type GrupoRemoto struct {
	ID           int    `json:"id"`
	MateriaClave string `json:"materia_clave"`
	Numero       string `json:"numero"`
	Dia          string `json:"dia"`
	HoraInicio   string `json:"hora_inicio"`
	HoraFin      string `json:"hora_fin"`
	CupoMaximo   int    `json:"cupo_maximo"`
	CupoOcupado  int    `json:"cupo_ocupado"`
}

// ServiciosConnector holds the base URLs of the services this one depends on.
// In production both point at the load balancer, so this service never knows
// which concrete instance answered.
type ServiciosConnector struct {
	AlumnosURL  string
	MateriasURL string
	Client      *http.Client
}

func NewServiciosConnector(alumnosURL, materiasURL string) *ServiciosConnector {
	return &ServiciosConnector{
		AlumnosURL:  alumnosURL,
		MateriasURL: materiasURL,
		// A timeout is mandatory: without it a hung service would block this
		// goroutine forever and eventually exhaust the whole service.
		Client: &http.Client{Timeout: 5 * time.Second},
	}
}

// pedir performs the request and decodes the JSON body into destino.
// notFound is returned when the remote answers 404, so the caller can tell
// "does not exist" apart from "service is down".
func (c *ServiciosConnector) pedir(metodo, url string, destino interface{}, notFound error) error {
	req, err := http.NewRequest(metodo, url, bytes.NewReader(nil))
	if err != nil {
		return err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrServicioCaido, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		if destino == nil {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(destino)
	case http.StatusNotFound:
		return notFound
	case http.StatusConflict:
		return ErrSinCupo
	default:
		cuerpo, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("respuesta %d del servicio remoto: %s",
			resp.StatusCode, string(cuerpo))
	}
}

func (c *ServiciosConnector) GetAlumno(matricula string) (*AlumnoRemoto, error) {
	var alumno AlumnoRemoto
	url := fmt.Sprintf("%s/alumnos/%s", c.AlumnosURL, matricula)
	if err := c.pedir(http.MethodGet, url, &alumno, ErrAlumnoNoEncontrado); err != nil {
		return nil, err
	}
	return &alumno, nil
}

func (c *ServiciosConnector) GetGrupo(id int) (*GrupoRemoto, error) {
	var grupo GrupoRemoto
	url := fmt.Sprintf("%s/grupos/%d", c.MateriasURL, id)
	if err := c.pedir(http.MethodGet, url, &grupo, ErrGrupoNoEncontrado); err != nil {
		return nil, err
	}
	return &grupo, nil
}

// ApartarLugar claims a seat in the materias service. That service owns the
// seat counter, so it is the one that guards it against overbooking.
func (c *ServiciosConnector) ApartarLugar(id int) error {
	url := fmt.Sprintf("%s/grupos/%d/apartar", c.MateriasURL, id)
	return c.pedir(http.MethodPost, url, nil, ErrGrupoNoEncontrado)
}

func (c *ServiciosConnector) LiberarLugar(id int) error {
	url := fmt.Sprintf("%s/grupos/%d/liberar", c.MateriasURL, id)
	return c.pedir(http.MethodPost, url, nil, ErrGrupoNoEncontrado)
}
