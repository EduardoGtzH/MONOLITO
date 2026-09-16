package models

import (
	"database/sql"
	"errors"
)

var ErrInscripcionNoEncontrada = errors.New("inscripcion no encontrada")

type Inscripcion struct {
	ID            int    `json:"id"`
	Matricula     string `json:"matricula"`
	GrupoID       int    `json:"grupo_id"`
	MateriaClave  string `json:"materia_clave"`
	Estado        string `json:"estado"`
	InscritoEn    string `json:"inscrito_en"`
}

type InscripcionModel struct {
	DB *sql.DB
}

const columnas = `id, matricula, grupo_id, materia_clave, estado,
                  TO_CHAR(inscrito_en, 'YYYY-MM-DD HH24:MI')`

// GetByMatricula returns a student's active enrollments.
func (m *InscripcionModel) GetByMatricula(matricula string) ([]Inscripcion, error) {
	query := `SELECT ` + columnas + ` FROM inscripciones
	          WHERE matricula = $1 AND estado = 'ACTIVA'
	          ORDER BY materia_clave`

	rows, err := m.DB.Query(query, matricula)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lista := make([]Inscripcion, 0)
	for rows.Next() {
		var i Inscripcion
		err := rows.Scan(&i.ID, &i.Matricula, &i.GrupoID, &i.MateriaClave,
			&i.Estado, &i.InscritoEn)
		if err != nil {
			return nil, err
		}
		lista = append(lista, i)
	}
	return lista, rows.Err()
}

// Create writes the enrollment row. The seat was already claimed in the
// materias service by the time this runs.
func (m *InscripcionModel) Create(matricula string, grupoID int, clave string) (*Inscripcion, error) {
	i := Inscripcion{Matricula: matricula, GrupoID: grupoID, MateriaClave: clave}
	query := `INSERT INTO inscripciones (matricula, grupo_id, materia_clave)
	          VALUES ($1, $2, $3)
	          RETURNING id, estado, TO_CHAR(inscrito_en, 'YYYY-MM-DD HH24:MI')`

	err := m.DB.QueryRow(query, matricula, grupoID, clave).
		Scan(&i.ID, &i.Estado, &i.InscritoEn)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// Baja marks an enrollment as dropped. It does not delete the row: keeping
// the history is what lets the university audit who dropped what and when.
func (m *InscripcionModel) Baja(matricula string, grupoID int) error {
	query := `UPDATE inscripciones SET estado = 'DADA_DE_BAJA'
	          WHERE matricula = $1 AND grupo_id = $2 AND estado = 'ACTIVA'`

	res, err := m.DB.Exec(query, matricula, grupoID)
	if err != nil {
		return err
	}
	filas, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if filas == 0 {
		return ErrInscripcionNoEncontrada
	}
	return nil
}
