package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/segmentio/kafka-go"

	"inscripciones.com/notificaciones/events"
	"inscripciones.com/notificaciones/models"
)

// NotificacionController tiene dos entradas: los mensajes de Kafka (Manejar)
// y la API HTTP para que el frontend muestre los avisos.
type NotificacionController struct {
	Instancia string
	Model     *models.NotificacionModel
}

// aviso es lo que se arma a partir de un evento: a quien va (vacio =
// coordinacion), el texto que vera en el frontend y la linea del "correo"
// que se escribe en el log.
type aviso struct {
	eventoID, matricula, mensaje, correo string
}

// Manejar recibe los eventos de los cuatro topicos. msg.Topic dice de cual
// vino, y con eso se sabe que estructura tiene el JSON.
func (c *NotificacionController) Manejar(_ context.Context, msg kafka.Message) error {
	var a aviso

	switch msg.Topic {
	case events.TopicoAlumnosRegistrados:
		var e events.AlumnoRegistrado
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			return c.ilegible(msg, err)
		}
		a = aviso{e.EventoID, e.Matricula,
			fmt.Sprintf("Bienvenido(a), %s. Ya estás registrado en %s.", e.Nombre, e.Carrera),
			fmt.Sprintf("bienvenida a %s (%s), alumno de %s", e.Nombre, e.Matricula, e.Carrera)}

	case events.TopicoInscripcionesCreadas:
		var e events.InscripcionCreada
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			return c.ilegible(msg, err)
		}
		a = aviso{e.EventoID, e.Matricula,
			fmt.Sprintf("Quedaste inscrito en %s.", e.MateriaClave),
			fmt.Sprintf("%s quedo inscrito en %s (grupo %d)", e.Matricula, e.MateriaClave, e.GrupoID)}
		if e.DesdeListaEspera {
			a.mensaje = fmt.Sprintf("Se liberó un lugar en %s y quedaste inscrito desde la lista de espera.",
				e.MateriaClave)
			a.correo += " desde la lista de espera"
		}

	case events.TopicoInscripcionesCanceladas:
		var e events.InscripcionCancelada
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			return c.ilegible(msg, err)
		}
		a = aviso{e.EventoID, e.Matricula,
			fmt.Sprintf("Registramos tu baja de %s.", e.MateriaClave),
			fmt.Sprintf("%s se dio de baja de %s (grupo %d)", e.Matricula, e.MateriaClave, e.GrupoID)}

	case events.TopicoGruposLlenos:
		var e events.GrupoLleno
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			return c.ilegible(msg, err)
		}
		a = aviso{e.EventoID, "",
			fmt.Sprintf("El grupo %s-%s ocupó sus %d lugares.", e.MateriaClave, e.Numero, e.CupoMaximo),
			fmt.Sprintf("a coordinacion: el grupo %s-%s ya ocupo sus %d lugares",
				e.MateriaClave, e.Numero, e.CupoMaximo)}

	default:
		return nil
	}

	nuevo, err := c.Model.Guardar(a.eventoID, a.matricula, msg.Topic, a.mensaje)
	if err != nil {
		return err // la base no contesto: kafkax lo reintenta
	}
	if !nuevo {
		log.Printf("[%s] evento %s repetido, no se manda otra vez", c.Instancia, a.eventoID)
		return nil
	}
	log.Printf("[%s] %s particion=%d offset=%d -> correo: %s",
		c.Instancia, msg.Topic, msg.Partition, msg.Offset, a.correo)
	return nil
}

// ilegible: un mensaje que no es JSON valido nunca va a funcionar, por mas
// que se reintente. Se registra y se devuelve nil para seguir con el resto.
func (c *NotificacionController) ilegible(msg kafka.Message, err error) error {
	log.Printf("[%s] mensaje ilegible en %s offset %d, se ignora: %v",
		c.Instancia, msg.Topic, msg.Offset, err)
	return nil
}

func responderJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// NotificacionesHandler: GET /notificaciones?matricula=A01234567
func (c *NotificacionController) NotificacionesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
		return
	}
	matricula := r.URL.Query().Get("matricula")
	if matricula == "" {
		http.Error(w, "falta el parametro 'matricula'", http.StatusBadRequest)
		return
	}
	lista, err := c.Model.DeAlumno(matricula)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	responderJSON(w, http.StatusOK, lista)
}

// CoordinacionHandler: GET /notificaciones/coordinacion
func (c *NotificacionController) CoordinacionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
		return
	}
	lista, err := c.Model.Coordinacion()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	responderJSON(w, http.StatusOK, lista)
}
