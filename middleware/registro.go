package main

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"sync/atomic"
)

var ErrServicioDesconocido = errors.New("servicio no registrado")

// definicionServicio is the shape of one entry in routes.json.
type definicionServicio struct {
	Prefijos   []string `json:"prefijos"`
	Instancias []string `json:"instancias"`
}

type documentoRutas struct {
	Servicios map[string]definicionServicio `json:"servicios"`
}

// Instancia is one concrete container. The health flag is atomic because the
// monitor goroutines write it while HTTP handlers read it, with no lock
// between them.
type Instancia struct {
	URL      string
	Servicio string
	sano     atomic.Bool
	fallos   atomic.Int64
}

func (i *Instancia) Sano() bool     { return i.sano.Load() }
func (i *Instancia) Fallos() int64  { return i.fallos.Load() }

// MarcarSano resets the failure counter and returns true if this is a
// transition from unhealthy, so the caller can log it once instead of
// on every successful poll.
func (i *Instancia) MarcarSano() bool {
	i.fallos.Store(0)
	return i.sano.Swap(true) == false
}

func (i *Instancia) MarcarCaido() bool {
	i.fallos.Add(1)
	return i.sano.Swap(false) == true
}

// Registro is the service discovery table. The RWMutex guards the maps,
// which only change when something registers at runtime. The health flags
// inside each Instancia are atomic and need no lock at all.
type Registro struct {
	mu        sync.RWMutex
	servicios map[string][]*Instancia
	prefijos  map[string]string // prefix -> service name
}

func NuevoRegistro() *Registro {
	return &Registro{
		servicios: make(map[string][]*Instancia),
		prefijos:  make(map[string]string),
	}
}

// CargarDesdeArchivo reads routes.json. Instances start marked healthy so the
// system is usable immediately; the first monitor tick corrects any that are
// actually down a few seconds later.
func (r *Registro) CargarDesdeArchivo(ruta string) error {
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return err
	}

	var doc documentoRutas
	if err := json.Unmarshal(datos, &doc); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for nombre, def := range doc.Servicios {
		lista := make([]*Instancia, 0, len(def.Instancias))
		for _, url := range def.Instancias {
			inst := &Instancia{URL: url, Servicio: nombre}
			inst.sano.Store(true)
			lista = append(lista, inst)
		}
		r.servicios[nombre] = lista
		for _, p := range def.Prefijos {
			r.prefijos[p] = nombre
		}
	}
	return nil
}

// Registrar adds an instance at runtime. This is the dynamic half of service
// discovery: a new container can announce itself instead of being listed in
// routes.json. Registering a URL that already exists is a no-op, which makes
// the call idempotent and safe to retry.
func (r *Registro) Registrar(servicio, url string, prefijos []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, inst := range r.servicios[servicio] {
		if inst.URL == url {
			return
		}
	}

	inst := &Instancia{URL: url, Servicio: servicio}
	inst.sano.Store(true)
	r.servicios[servicio] = append(r.servicios[servicio], inst)

	for _, p := range prefijos {
		r.prefijos[p] = servicio
	}
}

// InstanciasSanas returns only the URLs the load balancer may route to.
func (r *Registro) InstanciasSanas(servicio string) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lista, existe := r.servicios[servicio]
	if !existe {
		return nil, ErrServicioDesconocido
	}

	sanas := make([]string, 0, len(lista))
	for _, inst := range lista {
		if inst.Sano() {
			sanas = append(sanas, inst.URL)
		}
	}
	return sanas, nil
}

// Todas returns every instance, for the monitor and for /status.
func (r *Registro) Todas() []*Instancia {
	r.mu.RLock()
	defer r.mu.RUnlock()

	todas := make([]*Instancia, 0)
	for _, lista := range r.servicios {
		todas = append(todas, lista...)
	}
	sort.Slice(todas, func(a, b int) bool {
		if todas[a].Servicio != todas[b].Servicio {
			return todas[a].Servicio < todas[b].Servicio
		}
		return todas[a].URL < todas[b].URL
	})
	return todas
}

// Prefijos returns a copy of the prefix table for the load balancer.
func (r *Registro) Prefijos() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	copia := make(map[string]string, len(r.prefijos))
	for k, v := range r.prefijos {
		copia[k] = v
	}
	return copia
}
