package main

import (
	"log"
	"net/http"
	"sync"
	"time"
)

// Monitor polls every instance's /heartbeat on a fixed interval.
type Monitor struct {
	registro *Registro
	client   *http.Client
	intervalo time.Duration
}

func NuevoMonitor(r *Registro, intervalo time.Duration) *Monitor {
	return &Monitor{
		registro: r,
		// The timeout must be well below the interval, otherwise a hung
		// instance would still be pending when the next tick fires.
		client:    &http.Client{Timeout: 2 * time.Second},
		intervalo: intervalo,
	}
}

// Iniciar runs the monitor loop. It is meant to be launched with `go`.
func (m *Monitor) Iniciar() {
	ticker := time.NewTicker(m.intervalo)
	defer ticker.Stop()

	m.revisarTodas() // check once immediately instead of waiting a full tick
	for range ticker.C {
		m.revisarTodas()
	}
}

// revisarTodas polls every instance in parallel. One goroutine per instance
// means a slow one delays only itself: the whole round takes as long as the
// slowest poll, not the sum of all of them.
func (m *Monitor) revisarTodas() {
	instancias := m.registro.Todas()

	var wg sync.WaitGroup
	for _, inst := range instancias {
		wg.Add(1)
		go func(inst *Instancia) {
			defer wg.Done()
			m.revisar(inst)
		}(inst)
	}
	wg.Wait()
}

func (m *Monitor) revisar(inst *Instancia) {
	resp, err := m.client.Get(inst.URL + "/heartbeat")
	if err != nil {
		if inst.MarcarCaido() {
			log.Printf("CAIDO   %s (%s): %v", inst.URL, inst.Servicio, err)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if inst.MarcarCaido() {
			log.Printf("CAIDO   %s (%s): status %d", inst.URL, inst.Servicio, resp.StatusCode)
		}
		return
	}

	if inst.MarcarSano() {
		log.Printf("SANO    %s (%s)", inst.URL, inst.Servicio)
	}
}
