package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync"

	"inscripciones.com/notificaciones/controllers"
	"inscripciones.com/notificaciones/events"
	"inscripciones.com/notificaciones/kafkax"
)

var instanceID = "notificaciones-desconocido"

// heartbeatHandler lo consulta Consul cada 5 s (ver consul.go).
func heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("notificaciones alive: " + instanceID))
}

func main() {
	if v := os.Getenv("INSTANCE_ID"); v != "" {
		instanceID = v
	}

	controller := controllers.NewNotificacionController(instanceID)

	// Las colas que escucha este servicio: una goroutine por topico, todas
	// en el mismo consumer group. Asi las dos instancias de notificaciones
	// se reparten las 3 particiones de cada topico.
	topicos := []string{
		events.TopicoAlumnosRegistrados,
		events.TopicoInscripcionesCreadas,
		events.TopicoInscripcionesCanceladas,
		events.TopicoGruposLlenos,
	}

	ctx, cancelar := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, topico := range topicos {
		wg.Add(1)
		go func(topico string) {
			defer wg.Done()
			kafkax.Consume(ctx, kafkax.Brokers(), topico,
				events.ConsumidorNotificaciones, controller.Manejar)
		}(topico)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/heartbeat", heartbeatHandler)

	// Igual que en los otros servicios: registro en Consul y servidor HTTP.
	// Se queda aqui hasta que llega SIGTERM (docker stop).
	servirConRegistro(mux)

	// Ya apagado el HTTP, se apagan los consumidores. Cada uno sale de su
	// grupo, y Kafka le pasa sus particiones a la otra instancia al momento.
	cancelar()
	wg.Wait()
	log.Printf("%s apagado", instanceID)
}
