package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

var registro = NuevoRegistro()

func responderJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// resolveHandler is the endpoint the load balancer calls before every routing
// decision: GET /resolve?servicio=alumnos
func resolveHandler(w http.ResponseWriter, r *http.Request) {
	servicio := r.URL.Query().Get("servicio")
	if servicio == "" {
		http.Error(w, "falta el parametro 'servicio'", http.StatusBadRequest)
		return
	}

	instancias, err := registro.InstanciasSanas(servicio)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"servicio":   servicio,
		"instancias": instancias,
	})
}

// routesHandler gives the load balancer the prefix table so it can turn a
// request path into a service name.
func routesHandler(w http.ResponseWriter, r *http.Request) {
	responderJSON(w, http.StatusOK, registro.Prefijos())
}

type estadoInstancia struct {
	Servicio string `json:"servicio"`
	URL      string `json:"url"`
	Sano     bool   `json:"sano"`
	Fallos   int64  `json:"fallos_consecutivos"`
}

// statusHandler is the human-facing view of the registry.
func statusHandler(w http.ResponseWriter, r *http.Request) {
	todas := registro.Todas()
	estados := make([]estadoInstancia, 0, len(todas))
	sanas := 0

	for _, inst := range todas {
		if inst.Sano() {
			sanas++
		}
		estados = append(estados, estadoInstancia{
			Servicio: inst.Servicio,
			URL:      inst.URL,
			Sano:     inst.Sano(),
			Fallos:   inst.Fallos(),
		})
	}

	responderJSON(w, http.StatusOK, map[string]interface{}{
		"total":      len(todas),
		"sanas":      sanas,
		"caidas":     len(todas) - sanas,
		"instancias": estados,
	})
}

type peticionRegistro struct {
	Servicio string   `json:"servicio"`
	URL      string   `json:"url"`
	Prefijos []string `json:"prefijos"`
}

// registerHandler lets an instance announce itself at runtime instead of
// being listed in routes.json. This is the dynamic service discovery.
func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "metodo no permitido", http.StatusMethodNotAllowed)
		return
	}

	var p peticionRegistro
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "cuerpo invalido", http.StatusBadRequest)
		return
	}
	if p.Servicio == "" || p.URL == "" {
		http.Error(w, "servicio y url son obligatorios", http.StatusBadRequest)
		return
	}

	registro.Registrar(p.Servicio, p.URL, p.Prefijos)
	log.Printf("REGISTRO %s -> %s", p.Servicio, p.URL)
	responderJSON(w, http.StatusCreated, map[string]string{"estado": "registrado"})
}

func heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("middleware alive"))
}

func main() {
	archivo := os.Getenv("ROUTES_FILE")
	if archivo == "" {
		archivo = "routes.json"
	}
	if err := registro.CargarDesdeArchivo(archivo); err != nil {
		log.Fatalf("no se pudo cargar %s: %v", archivo, err)
	}
	log.Printf("service discovery cargado desde %s", archivo)

	monitor := NuevoMonitor(registro, 5*time.Second)
	go monitor.Iniciar()

	mux := http.NewServeMux()
	mux.HandleFunc("/resolve", resolveHandler)
	mux.HandleFunc("/routes", routesHandler)
	mux.HandleFunc("/status", statusHandler)
	mux.HandleFunc("/register", registerHandler)
	mux.HandleFunc("/heartbeat", heartbeatHandler)

	puerto := os.Getenv("PUERTO")
	if puerto == "" {
		puerto = "8500"
	}
	log.Printf("middleware escuchando en :%s", puerto)
	log.Fatal(http.ListenAndServe(":"+puerto, mux))
}
