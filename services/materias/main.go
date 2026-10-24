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

	"inscripciones.com/materias/connectors"
	"inscripciones.com/materias/controllers"
	"inscripciones.com/materias/events"
	"inscripciones.com/materias/kafkax"
	"inscripciones.com/materias/models"

	_ "github.com/lib/pq"
)

// instanceID identifies which container answered a request. Set via the
// INSTANCE_ID env var in docker-compose.yml so the load balancer's work
// is visible from the client side.
var instanceID = "materias-desconocido"

// withInstanceHeader tags every response with X-Instance-Id.
func withInstanceHeader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Instance-Id", instanceID)
		next(w, r)
	}
}

// heartbeatHandler is polled by the middleware every few seconds. If this
// stops answering 200, the middleware marks the instance unhealthy and the
// load balancer stops routing to it.
func heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("materias alive: " + instanceID))
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

	materiaModel := &models.MateriaModel{DB: db}
	if err := materiaModel.AsegurarTablaEventos(); err != nil {
		log.Fatalf("no se pudo crear la tabla eventos_procesados: %v", err)
	}

	// Productor de Kafka. defer Close: al apagar, manda lo pendiente.
	eventos := connectors.NewEventosConnector(kafkax.Brokers())
	defer eventos.Close()

	materiaController := &controllers.MateriaController{
		MateriaModel: materiaModel,
		Eventos:      eventos,
	}
	eventosController := &controllers.EventosController{MateriaModel: materiaModel}

	// Consumidor: grupo "materias" sobre inscripciones.canceladas. Las 3
	// instancias usan el mismo grupo, asi que cada baja la procesa solo una.
	ctx, cancelar := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		kafkax.Consume(ctx, kafkax.Brokers(), events.TopicoInscripcionesCanceladas,
			events.ConsumidorMaterias, eventosController.InscripcionCancelada)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/materias", withInstanceHeader(materiaController.MateriasHandler))
	mux.HandleFunc("/materias/", withInstanceHeader(materiaController.MateriaHandler))
	mux.HandleFunc("/grupos/", withInstanceHeader(materiaController.GrupoHandler))
	mux.HandleFunc("/heartbeat", withInstanceHeader(heartbeatHandler))

	// El registro en Consul, el apagado limpio y el servidor viven en
	// consul.go para que todo lo relacionado con discovery este junto.
	servirConRegistro(mux)

	// Ya apagado el HTTP, se apaga el consumidor: sale del grupo y Kafka le
	// pasa su particion a otra instancia de materias al momento.
	cancelar()
	wg.Wait()
}
