package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"inscripciones.com/materias/controllers"
	"inscripciones.com/materias/models"

	_ "github.com/lib/pq"
)

var instanceID = "materias-desconocido"

func withInstanceHeader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Instance-Id", instanceID)
		next(w, r)
	}
}

func heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("materias alive: " + instanceID))
}

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
	materiaController := &controllers.MateriaController{MateriaModel: materiaModel}

	mux := http.NewServeMux()
	mux.HandleFunc("/materias", withInstanceHeader(materiaController.MateriasHandler))
	mux.HandleFunc("/materias/", withInstanceHeader(materiaController.MateriaHandler))
	mux.HandleFunc("/grupos/", withInstanceHeader(materiaController.GrupoHandler))
	mux.HandleFunc("/heartbeat", withInstanceHeader(heartbeatHandler))

	log.Printf("servicio %s escuchando en :8080", instanceID)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
