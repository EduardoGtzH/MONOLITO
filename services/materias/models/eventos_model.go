package models

import "time"

// Este archivo es parte del mismo modelo (MateriaModel): son los metodos que
// usa el consumidor de Kafka. Estan aparte solo para no mezclarlos con los
// que usa la API HTTP.

// AsegurarTablaEventos crea la tabla donde materias anota los eventos que ya
// proceso. Se crea al arrancar y no en db/init porque esos scripts solo
// corren la primera vez que se crea el volumen de Postgres.
func (m *MateriaModel) AsegurarTablaEventos() error {
	var err error
	for intento := 1; intento <= 3; intento++ {
		_, err = m.DB.Exec(`CREATE TABLE IF NOT EXISTS eventos_procesados (
			evento_id    VARCHAR(36) PRIMARY KEY,
			procesado_en TIMESTAMP   NOT NULL DEFAULT NOW()
		)`)
		if err == nil {
			return nil
		}
		// Las 3 instancias arrancan juntas. Si dos crean la tabla en el mismo
		// instante, Postgres rechaza a una; al reintentar ya existe.
		time.Sleep(time.Second)
	}
	return err
}

// LiberarPorEvento devuelve un lugar porque llego una baja por Kafka. Es
// idempotente: si el mismo evento llega dos veces, el lugar se devuelve una
// sola vez.
//
// Las dos cosas van en UNA transaccion: anotar el evento_id (PRIMARY KEY) y
// bajar el contador. Si el evento ya estaba anotado, el INSERT no inserta
// nada y el contador no se toca. Si algo falla a la mitad, el rollback
// deshace las dos y el evento se reintenta completo.
func (m *MateriaModel) LiberarPorEvento(eventoID string, grupoID int) (bool, error) {
	tx, err := m.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback() // no-op once Commit succeeds

	res, err := tx.Exec(`INSERT INTO eventos_procesados (evento_id) VALUES ($1)
	                     ON CONFLICT (evento_id) DO NOTHING`, eventoID)
	if err != nil {
		return false, err
	}
	nuevas, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if nuevas == 0 {
		return false, nil // repetido: ya se habia procesado
	}

	_, err = tx.Exec(`UPDATE grupos SET cupo_ocupado = GREATEST(cupo_ocupado - 1, 0)
	                  WHERE id = $1`, grupoID)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}
