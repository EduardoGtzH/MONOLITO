package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"inscripciones.com/alumnos/controllers"
	"inscripciones.com/alumnos/models"

	_ "github.com/lib/pq"
)

// instanceID identifies which container answered a request. Set via the
// INSTANCE_ID env var in docker-compose.yml so the load balancer's work
// is visible from the client side.
var instanceID = "alumnos-desconocido"

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
	w.Write([]byte("alumnos alive: " + instanceID))
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

	// Wire up the MVC components.
	alumnoModel := &models.AlumnoModel{DB: db}
	alumnoController := &controllers.AlumnoController{AlumnoModel: alumnoModel}

	mux := http.NewServeMux()
	mux.HandleFunc("/alumnos", withInstanceHeader(alumnoController.AlumnosHandler))
	mux.HandleFunc("/alumnos/", withInstanceHeader(alumnoController.AlumnoHandler))
	mux.HandleFunc("/heartbeat", withInstanceHeader(heartbeatHandler))

	log.Printf("servicio %s escuchando en :8080", instanceID)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
