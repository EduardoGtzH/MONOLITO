// Package kafkax envuelve segmentio/kafka-go con las pocas funciones que usan
// los servicios. Es el mismo archivo en cada servicio, igual que consul.go:
// cada servicio es su propio modulo de Go y no comparten codigo.
//
// Esta basado en kafkax.go de kafka_example de la clase. La diferencia es
// que aqui los topicos no los crea el codigo: los crea el contenedor
// kafka-init al arrancar (ver kafka/crear-topicos.sh).
package kafkax

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

// Brokers lee KAFKA_BROKER del entorno. Puede ser una direccion o varias
// separadas por comas; en este proyecto solo hay un broker: kafka:9092.
func Brokers() []string {
	v := os.Getenv("KAFKA_BROKER")
	if v == "" {
		v = "kafka:9092"
	}
	return strings.Split(v, ",")
}

// ------------------------------------------------------------ productor

// NewWriter crea un productor para UN topico.
func NewWriter(brokers []string, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:  kafka.TCP(brokers...),
		Topic: topic,
		// particion = hash(clave). Todos los eventos con la misma clave caen
		// en la misma particion, y dentro de una particion Kafka respeta el
		// orden. Con la matricula como clave, los eventos de un alumno se
		// leen en el orden en que pasaron.
		Balancer: &kafka.Hash{},
		// El broker solo responde "listo" cuando ya guardo el evento.
		RequiredAcks: kafka.RequireAll,
		BatchTimeout: 10 * time.Millisecond,
	}
}

// Publish convierte el evento a JSON y lo escribe en el topico del writer.
// Espera como maximo 5 segundos: si Kafka no responde, quien llamo recibe el
// error en lugar de quedarse colgado.
//
// El error quiere decir "Kafka no confirmo", no "seguro no salio": el writer
// sigue reintentando unos segundos por su cuenta. Si Kafka regresa pronto, el
// evento llega tarde; si tarda mas, se pierde.
func Publish(w *kafka.Writer, clave string, evento interface{}) error {
	valor, err := json.Marshal(evento)
	if err != nil {
		return err
	}
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	return w.WriteMessages(ctx, kafka.Message{
		Key:   []byte(clave),
		Value: valor,
	})
}

// ------------------------------------------------------------ consumidor

// Handler procesa un mensaje. Devolver error significa "fallo, reintentalo".
type Handler func(ctx context.Context, msg kafka.Message) error

// Consume se une a un consumer group y procesa el topico hasta que ctx se
// cancela.
//
// Consumer group = cola. Las instancias con el MISMO groupID se reparten las
// particiones del topico: cada evento lo procesa solo una de ellas. Grupos
// DISTINTOS reciben cada uno su propia copia de todos los eventos.
//
// El offset se confirma (commit) DESPUES de que el handler termina bien. Si
// el servicio muere a la mitad, el evento se vuelve a entregar al regresar:
// entrega "al menos una vez". Por eso los handlers deben ser idempotentes:
// procesar dos veces el mismo evento no debe dar un resultado distinto.
func Consume(ctx context.Context, brokers []string, topic, groupID string, handle Handler) {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers,
		Topic:   topic,
		GroupID: groupID,
		// Un grupo nuevo empieza desde el evento mas viejo, para no perder
		// lo que se publico antes de que el consumidor existiera.
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
		// Cada pedido de mensajes espera a lo mucho 1 s a que lleguen datos.
		// El default de kafka-go es 10 s, y mientras un pedido espera, la
		// instancia no puede soltar ni recibir particiones: al apagar una
		// instancia, la otra tardaba hasta 15 s en tomar su lugar.
		MaxWait: time.Second,
		// El nombre de la instancia viaja como client id. Asi
		// kafka-consumer-groups.sh muestra que contenedor tiene cada particion.
		Dialer: &kafka.Dialer{
			ClientID:  os.Getenv("INSTANCE_ID"),
			Timeout:   10 * time.Second,
			DualStack: true,
		},
	})
	// Close avisa al grupo que esta instancia se va, y sus particiones pasan
	// a las demas al momento en lugar de esperar 30 segundos.
	defer r.Close()

	log.Printf("[kafka] grupo %q escuchando el topico %s", groupID, topic)
	for {
		msg, err := r.FetchMessage(ctx) // a diferencia de ReadMessage, no hace commit solo
		if err != nil {
			if ctx.Err() != nil {
				return // apagado normal
			}
			log.Printf("[kafka] error leyendo %s: %v", topic, err)
			time.Sleep(time.Second)
			continue
		}

		if err := processWithRetry(ctx, handle, msg, 3); err != nil {
			if ctx.Err() != nil {
				// Nos estamos apagando a la mitad: sin commit, el evento se
				// le entrega a otra instancia o a esta misma cuando regrese.
				return
			}
			// En produccion se mandaria a un topico de "cartas muertas"
			// (dead letter queue) para revisarlo a mano.
			log.Printf("[kafka] se descarta %s particion=%d offset=%d: %v",
				topic, msg.Partition, msg.Offset, err)
		}

		if err := r.CommitMessages(ctx, msg); err != nil && ctx.Err() == nil {
			// Si el commit falla, el evento puede llegar otra vez. Como los
			// handlers son idempotentes, no pasa nada.
			log.Printf("[kafka] no se confirmo el offset %d de %s: %v", msg.Offset, topic, err)
		}
	}
}

// processWithRetry intenta el handler varias veces, esperando un poco mas
// cada vez (0.5 s, 1 s, ...).
func processWithRetry(ctx context.Context, h Handler, msg kafka.Message, intentos int) error {
	var err error
	for i := 1; i <= intentos; i++ {
		if err = h(ctx, msg); err == nil {
			return nil
		}
		log.Printf("[kafka] intento %d/%d fallo: %v", i, intentos, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i) * 500 * time.Millisecond):
		}
	}
	return err
}
