package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// servicioConsul is the logical name every instance of this container
// registers under. Consul groups instances by this name, so alumnos-1,
// alumnos-2 and alumnos-3 all answer to "alumnos".
const servicioConsul = "inscripciones"

func direccionConsul() string {
	if v := os.Getenv("CONSUL_HTTP_ADDR"); v != "" {
		return v
	}
	return "http://consul:8500"
}

// registrarEnConsul tells Consul "I exist, here is how to reach me, and
// here is how to check whether I am healthy."
//
// The Check block is the important part: Consul takes over the health
// polling from here on. The middleware's own ticker, the gateway's
// heartbeat loop — none of that is needed anymore, because Consul does it
// once, centrally, for every consumer.
//
// Address is the container name, which Docker's embedded DNS resolves on
// the shared network. It retries because depends_on only guarantees the
// consul container has started, not that its API is accepting requests.
func registrarEnConsul() error {
	payload := map[string]interface{}{
		"ID":      instanceID,
		"Name":    servicioConsul,
		"Address": instanceID,
		"Port":    8080,
		"Check": map[string]interface{}{
			"HTTP":     fmt.Sprintf("http://%s:8080/heartbeat", instanceID),
			"Interval": "5s",
			"Timeout":  "2s",
			// If the check stays critical this long, Consul removes the
			// service on its own. It is the safety net for an instance
			// that died without deregistering.
			"DeregisterCriticalServiceAfter": "1m",
		},
	}

	cuerpo, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	addr := direccionConsul()
	cliente := &http.Client{Timeout: 3 * time.Second}
	var ultimoErr error

	for intento := 1; intento <= 10; intento++ {
		req, err := http.NewRequest(http.MethodPut,
			addr+"/v1/agent/service/register", bytes.NewReader(cuerpo))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := cliente.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				log.Printf("registrado %s en consul (%s) como servicio %q",
					instanceID, addr, servicioConsul)
				return nil
			}
			ultimoErr = fmt.Errorf("consul respondio %d", resp.StatusCode)
		} else {
			ultimoErr = err
		}
		time.Sleep(time.Duration(intento) * time.Second)
	}
	return fmt.Errorf("no se pudo registrar en consul: %w", ultimoErr)
}

// desregistrarDeConsul removes this instance from the registry. Called on
// shutdown so consumers stop routing here immediately, instead of waiting
// for the health check to fail and DeregisterCriticalServiceAfter to
// expire.
func desregistrarDeConsul() {
	addr := direccionConsul()
	req, err := http.NewRequest(http.MethodPut,
		addr+"/v1/agent/service/deregister/"+instanceID, nil)
	if err != nil {
		log.Printf("no se pudo construir la peticion de borrado: %v", err)
		return
	}

	cliente := &http.Client{Timeout: 3 * time.Second}
	resp, err := cliente.Do(req)
	if err != nil {
		log.Printf("no se pudo desregistrar %s de consul: %v", instanceID, err)
		return
	}
	resp.Body.Close()
	log.Printf("desregistrado %s de consul", instanceID)
}

// servirConRegistro ties the HTTP server's lifecycle to the Consul
// registration: register, serve, and deregister cleanly on shutdown.
//
// docker stop sends SIGTERM. Catching it is what makes the removal
// immediate; without this the instance would linger in the registry as
// "critical" for up to a minute.
func servirConRegistro(mux *http.ServeMux) {
	if err := registrarEnConsul(); err != nil {
		// Not fatal: the service still answers requests, it is just
		// invisible to discovery. Logged loudly because running
		// undiscoverable is a confusing failure mode.
		log.Printf("ADVERTENCIA: %v", err)
	}

	srv := &http.Server{Addr: ":8080", Handler: mux}

	senales := make(chan os.Signal, 1)
	signal.Notify(senales, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-senales
		log.Printf("apagando %s", instanceID)
		desregistrarDeConsul()
		ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		srv.Shutdown(ctx)
	}()

	log.Printf("servicio %s escuchando en :8080", instanceID)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
