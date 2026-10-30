package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"inscripciones.com/inscripciones/connectors"
	"inscripciones.com/inscripciones/controllers"
	"inscripciones.com/inscripciones/events"
	"inscripciones.com/inscripciones/kafkax"
	"inscripciones.com/inscripciones/models"

	_ "github.com/lib/pq"
)

var instanceID = "inscripciones-desconocido"

func withInstanceHeader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Instance-Id", instanceID)
		next(w, r)
	}
}

func heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("inscripciones alive: " + instanceID))
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

// leerReglas arma las reglas de la cadena desde el entorno:
//
//	MAX_CREDITOS=30                    limite de creditos por alumno (0 = sin limite)
//	INSCRIPCION_INICIO=2026-08-01      desde cuando se puede inscribir (vacio = siempre)
//	INSCRIPCION_FIN=2026-12-15 23:59   hasta cuando (vacio = siempre)
func leerReglas() models.Reglas {
	reglas := models.Reglas{MaxCreditos: 30}
	if v := os.Getenv("MAX_CREDITOS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("MAX_CREDITOS invalido: %q", v)
		}
		reglas.MaxCreditos = n
	}
	fecha := func(nombre string) time.Time {
		v := os.Getenv(nombre)
		if v == "" {
			return time.Time{}
		}
		for _, formato := range []string{"2006-01-02 15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(formato, v, time.Local); err == nil {
				return t
			}
		}
		log.Fatalf("%s invalido: %q (usa AAAA-MM-DD o AAAA-MM-DD HH:MM)", nombre, v)
		return time.Time{}
	}
	reglas.Inicio = fecha("INSCRIPCION_INICIO")
	reglas.Fin = fecha("INSCRIPCION_FIN")
	return reglas
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

	conector := connectors.NewServiciosConnector(
		os.Getenv("ALUMNOS_URL"),
		os.Getenv("MATERIAS_URL"),
	)
	log.Printf("alumnos en %s, materias en %s",
		os.Getenv("ALUMNOS_URL"), os.Getenv("MATERIAS_URL"))

	// Productor de Kafka. defer Close: al apagar, manda lo pendiente.
	eventos := connectors.NewEventosConnector(kafkax.Brokers())
	defer eventos.Close()
	log.Printf("publicando eventos en kafka %v", kafkax.Brokers())

	reglas := leerReglas()
	fechaLegible := func(t time.Time) string {
		if t.IsZero() {
			return "sin limite"
		}
		return t.Format("2006-01-02 15:04")
	}
	log.Printf("reglas: maximo %d creditos, periodo de inscripcion: desde %s hasta %s",
		reglas.MaxCreditos, fechaLegible(reglas.Inicio), fechaLegible(reglas.Fin))

	inscripcionModel := &models.InscripcionModel{DB: db}
	proceso := &models.Proceso{
		Model:     inscripcionModel,
		Fila:      &models.FilaModel{DB: db},
		Connector: conector,
		Eventos:   eventos,
		Reglas:    reglas,
	}
	controller := &controllers.InscripcionController{Proceso: proceso}
	eventosController := &controllers.EventosController{Proceso: proceso}

	// Consumidor: grupo "inscripciones" sobre lugares.liberados. Cuando
	// materias devuelve un lugar, aqui se inscribe al primero de la fila.
	ctx, cancelar := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		kafkax.Consume(ctx, kafkax.Brokers(), events.TopicoLugaresLiberados,
			events.ConsumidorInscripciones, eventosController.LugarLiberado)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/inscripciones", withInstanceHeader(controller.InscripcionesHandler))
	mux.HandleFunc("/inscripciones/", withInstanceHeader(controller.BajaHandler))
	mux.HandleFunc("/lista-espera", withInstanceHeader(controller.ListaEsperaHandler))
	mux.HandleFunc("/lista-espera/", withInstanceHeader(controller.SalirDeFilaHandler))
	mux.HandleFunc("/heartbeat", withInstanceHeader(heartbeatHandler))

	// El registro en Consul, el apagado limpio y el servidor viven en
	// consul.go para que todo lo relacionado con discovery este junto.
	servirConRegistro(mux)

	// Ya apagado el HTTP, se apaga el consumidor: sale del grupo y Kafka le
	// pasa su particion a otra instancia al momento.
	cancelar()
	wg.Wait()
}
