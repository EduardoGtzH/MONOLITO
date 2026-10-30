package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"inscripciones.com/notificaciones/controllers"
	"inscripciones.com/notificaciones/events"
	"inscripciones.com/notificaciones/kafkax"
	"inscripciones.com/notificaciones/models"

	_ "github.com/lib/pq"
)

var instanceID = "notificaciones-desconocido"

// withInstanceHeader tags every response with X-Instance-Id, como los demas
// servicios: asi la bitacora del frontend muestra que instancia contesto.
func withInstanceHeader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Instance-Id", instanceID)
		next(w, r)
	}
}

// heartbeatHandler lo consulta Consul cada 5 s (ver consul.go).
func heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("notificaciones alive: " + instanceID))
}

// conectarDB retries the connection because the service container can start
// before Postgres finishes its init scripts.
func conectarDB(connStr string) *sql.DB {
	for intento := 1; intento <= 15; intento++ {
		db, err := sql.Open("postgres", connStr)
		if err == nil {
			if err = db.Ping(); err == nil {
				log.Println("conectado a la base de datos")
				return db
			}
			db.Close()
		}
		log.Printf("base de datos no lista (intento %d/15): %v", intento, err)
		time.Sleep(2 * time.Second)
	}
	log.Fatal("no se pudo conectar a la base de datos")
	return nil
}

func main() {
	if v := os.Getenv("INSTANCE_ID"); v != "" {
		instanceID = v
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"))
	db := conectarDB(connStr)
	defer db.Close()

	controller := &controllers.NotificacionController{
		Instancia: instanceID,
		Model:     &models.NotificacionModel{DB: db},
	}

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
	mux.HandleFunc("/notificaciones", withInstanceHeader(controller.NotificacionesHandler))
	mux.HandleFunc("/notificaciones/coordinacion", withInstanceHeader(controller.CoordinacionHandler))
	mux.HandleFunc("/heartbeat", withInstanceHeader(heartbeatHandler))

	// Igual que en los otros servicios: registro en Consul y servidor HTTP.
	// Se queda aqui hasta que llega SIGTERM (docker stop).
	servirConRegistro(mux)

	// Ya apagado el HTTP, se apagan los consumidores. Cada uno sale de su
	// grupo, y Kafka le pasa sus particiones a la otra instancia al momento.
	cancelar()
	wg.Wait()
	log.Printf("%s apagado", instanceID)
}
