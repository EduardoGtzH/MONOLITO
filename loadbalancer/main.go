package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

var balanceador *Balanceador

// sem is a counting semaphore used as a worker pool: it caps how many
// requests are proxied at once. Without it a traffic spike would open
// unbounded connections to the backends and take them down instead of
// just slowing this one.
var sem chan struct{}

// responseWriterVigilado tracks whether anything was written yet. A retry is
// only safe before the first byte goes out to the client.
type responseWriterVigilado struct {
	http.ResponseWriter
	escribio bool
}

func (w *responseWriterVigilado) WriteHeader(status int) {
	w.escribio = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriterVigilado) Write(b []byte) (int, error) {
	w.escribio = true
	return w.ResponseWriter.Write(b)
}

// proxyHandler is the whole load balancing path.
func proxyHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Take a slot from the worker pool, or shed the request.
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	case <-time.After(5 * time.Second):
		http.Error(w, "balanceador saturado, intenta de nuevo", http.StatusServiceUnavailable)
		return
	}

	// 2. Path -> service name, using the prefix table from the middleware.
	servicio, err := balanceador.servicioDe(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// 3. Service name -> healthy instances, from the middleware.
	instancias, err := balanceador.instancias(servicio)
	if err != nil {
		if errors.Is(err, ErrSinInstancias) {
			http.Error(w, "servicio "+servicio+" sin instancias disponibles",
				http.StatusServiceUnavailable)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// 4. Round robin, then try each instance in turn until one answers.
	candidatas := balanceador.ordenRoundRobin(servicio, instancias)
	vigilado := &responseWriterVigilado{ResponseWriter: w}

	for _, destino := range candidatas {
		fallo := proxear(vigilado, r, destino, servicio)
		if !fallo {
			return
		}
		if vigilado.escribio {
			// Headers already went out; we cannot start over.
			return
		}
		log.Printf("instancia %s no respondio, intentando la siguiente", destino)
	}

	http.Error(w, "ninguna instancia de "+servicio+" respondio", http.StatusBadGateway)
}

// proxear forwards the request to one backend. It returns true when the
// backend could not be reached, which is the signal to try the next one.
func proxear(w *responseWriterVigilado, r *http.Request, destino, servicio string) bool {
	target, err := url.Parse(destino)
	if err != nil {
		return true
	}

	fallo := false
	proxy := httputil.NewSingleHostReverseProxy(target)

	// ErrorHandler fires only on transport errors, before anything is
	// written, so it is safe to swallow the failure and let the caller retry.
	proxy.ErrorHandler = func(http.ResponseWriter, *http.Request, error) {
		fallo = true
	}

	// Headers that make the routing decision visible from the client side.
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("X-Load-Balancer", "loadbalancer")
		resp.Header.Set("X-Backend-Target", destino)
		resp.Header.Set("X-Backend-Service", servicio)
		return nil
	}

	proxy.ServeHTTP(w, r)
	return fallo
}

func heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("loadbalancer alive"))
}

// conCORS authorizes the browser to call this API from another origin.
// Access-Control-Expose-Headers is the key line: without it the browser
// receives X-Instance-Id but refuses to let JavaScript read it, and the
// frontend could not show which instance answered.
func conCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Expose-Headers",
			"X-Instance-Id, X-Backend-Target, X-Backend-Service")

		// The browser sends a preflight OPTIONS before any POST or DELETE.
		// It must be answered here, not forwarded to a backend.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func main() {
	middlewareURL := os.Getenv("MIDDLEWARE_URL")
	if middlewareURL == "" {
		middlewareURL = "http://middleware:8500"
	}
	balanceador = NuevoBalanceador(middlewareURL)

	sem = make(chan struct{}, 50)

	// Retry at startup: this container can win the race against the middleware.
	for intento := 1; intento <= 15; intento++ {
		if err := balanceador.RefrescarRutas(); err == nil {
			log.Println("rutas cargadas desde el middleware")
			break
		} else {
			log.Printf("middleware no disponible (intento %d/15): %v", intento, err)
			time.Sleep(2 * time.Second)
		}
	}

	// Keep the prefix table fresh so runtime registrations become routable.
	go func() {
		for range time.Tick(10 * time.Second) {
			if err := balanceador.RefrescarRutas(); err != nil {
				log.Printf("no se pudieron refrescar las rutas: %v", err)
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/heartbeat", conCORS(heartbeatHandler))
	mux.HandleFunc("/lb/rutas", conCORS(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(balanceador.rutas)
	}))
	mux.HandleFunc("/", conCORS(proxyHandler))

	puerto := os.Getenv("PUERTO")
	if puerto == "" {
		puerto = "8000"
	}
	log.Printf("load balancer escuchando en :%s, middleware en %s", puerto, middlewareURL)
	log.Fatal(http.ListenAndServe(":"+puerto, mux))
}
