package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrSinRuta       = errors.New("ninguna ruta coincide con esta peticion")
	ErrSinInstancias = errors.New("no hay instancias sanas para este servicio")
)

// entradaCache holds a resolve() answer for a short window. Asking the
// middleware on literally every request would add a round trip to each one;
// a two second TTL keeps failover fast while cutting that cost.
type entradaCache struct {
	instancias []string
	expira     time.Time
}

// Balanceador turns a request path into a concrete backend URL.
type Balanceador struct {
	middlewareURL string
	client        *http.Client

	mu     sync.RWMutex
	rutas  map[string]string       // prefix -> service name
	cache  map[string]entradaCache // service name -> healthy instances

	contadores sync.Map // service name -> *uint64, the round robin cursor
	ttl        time.Duration
}

func NuevoBalanceador(middlewareURL string) *Balanceador {
	return &Balanceador{
		middlewareURL: middlewareURL,
		client:        &http.Client{Timeout: 3 * time.Second},
		rutas:         make(map[string]string),
		cache:         make(map[string]entradaCache),
		ttl:           2 * time.Second,
	}
}

// RefrescarRutas pulls the prefix table from the middleware. Called at
// startup and then on a ticker, so a service registered at runtime becomes
// routable without restarting this container.
func (b *Balanceador) RefrescarRutas() error {
	resp, err := b.client.Get(b.middlewareURL + "/routes")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var rutas map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&rutas); err != nil {
		return err
	}

	b.mu.Lock()
	b.rutas = rutas
	b.mu.Unlock()
	return nil
}

// servicioDe matches the longest prefix that the path starts with. Longest
// wins so that a future "/materias/especiales" could be split off from
// "/materias" without ambiguity.
func (b *Balanceador) servicioDe(ruta string) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	prefijos := make([]string, 0, len(b.rutas))
	for p := range b.rutas {
		prefijos = append(prefijos, p)
	}
	sort.Slice(prefijos, func(i, j int) bool {
		return len(prefijos[i]) > len(prefijos[j])
	})

	for _, p := range prefijos {
		if ruta == p || strings.HasPrefix(ruta, p+"/") || strings.HasPrefix(ruta, p+"?") {
			return b.rutas[p], nil
		}
	}
	return "", ErrSinRuta
}

// instancias returns the healthy backends for a service, from cache when it
// is still fresh, otherwise from the middleware.
func (b *Balanceador) instancias(servicio string) ([]string, error) {
	b.mu.RLock()
	entrada, hay := b.cache[servicio]
	b.mu.RUnlock()

	if hay && time.Now().Before(entrada.expira) {
		return entrada.instancias, nil
	}

	url := fmt.Sprintf("%s/resolve?servicio=%s", b.middlewareURL, servicio)
	resp, err := b.client.Get(url)
	if err != nil {
		// The middleware is unreachable. Serving a stale list is better than
		// serving nothing: the instances were healthy seconds ago.
		if hay {
			return entrada.instancias, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("middleware respondio %d para %s", resp.StatusCode, servicio)
	}

	var respuesta struct {
		Instancias []string `json:"instancias"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respuesta); err != nil {
		return nil, err
	}

	b.mu.Lock()
	b.cache[servicio] = entradaCache{
		instancias: respuesta.Instancias,
		expira:     time.Now().Add(b.ttl),
	}
	b.mu.Unlock()

	if len(respuesta.Instancias) == 0 {
		return nil, ErrSinInstancias
	}
	return respuesta.Instancias, nil
}

// ordenRoundRobin returns the instances rotated so the next one in the cycle
// comes first. Returning the whole rotated list instead of a single pick is
// what lets the caller retry down the list when a backend fails.
//
// The cursor is a plain uint64 bumped with atomic.AddUint64: no mutex, and
// concurrent requests are guaranteed to get distinct consecutive numbers.
func (b *Balanceador) ordenRoundRobin(servicio string, instancias []string) []string {
	valor, _ := b.contadores.LoadOrStore(servicio, new(uint64))
	contador := valor.(*uint64)

	n := uint64(len(instancias))
	inicio := (atomic.AddUint64(contador, 1) - 1) % n

	ordenadas := make([]string, 0, n)
	for i := uint64(0); i < n; i++ {
		ordenadas = append(ordenadas, instancias[(inicio+i)%n])
	}
	return ordenadas
}
