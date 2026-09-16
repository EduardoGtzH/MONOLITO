package models

import (
	"database/sql"
	"fmt"
)

// Alumno is the resource this service owns. The json tags define exactly
// what the API returns; the db columns are read by hand in each query below.
type Alumno struct {
	ID               int    `json:"id"`
	Matricula        string `json:"matricula"`
	Nombre           string `json:"nombre"`
	Carrera          string `json:"carrera"`
	Semestre         int    `json:"semestre"`
	CreditosCursados int    `json:"creditos_cursados"`
	Activo           bool   `json:"activo"`
}

// AlumnoModel is the M in MVC: it holds the database handle and is the only
// place in this service where SQL is written.
//
// *sql.DB is already a connection pool and is safe for concurrent use, so the
// goroutine net/http spawns per request can share this single instance without
// any mutex of our own.
type AlumnoModel struct {
	DB *sql.DB
}

const columnas = `id, matricula, nombre, carrera, semestre, creditos_cursados, activo`

// GetAll returns every active student.
func (m *AlumnoModel) GetAll() ([]Alumno, error) {
	query := `SELECT ` + columnas + ` FROM alumnos WHERE activo = TRUE ORDER BY matricula`

	rows, err := m.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	alumnos := []Alumno{}
	for rows.Next() {
		var a Alumno
		err := rows.Scan(&a.ID, &a.Matricula, &a.Nombre, &a.Carrera,
			&a.Semestre, &a.CreditosCursados, &a.Activo)
		if err != nil {
			return nil, err
		}
		alumnos = append(alumnos, a)
	}
	return alumnos, rows.Err()
}

// GetByMatricula looks a student up by their school ID. This is the method
// the inscripciones service calls over HTTP to verify a student exists.
func (m *AlumnoModel) GetByMatricula(matricula string) (*Alumno, error) {
	alumno := &Alumno{}
	query := `SELECT ` + columnas + ` FROM alumnos WHERE matricula = $1`

	err := m.DB.QueryRow(query, matricula).Scan(&alumno.ID, &alumno.Matricula,
		&alumno.Nombre, &alumno.Carrera, &alumno.Semestre,
		&alumno.CreditosCursados, &alumno.Activo)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("alumno no encontrado")
		}
		return nil, err
	}
	return alumno, nil
}

// Create inserts a student and returns it with the id Postgres assigned.
// RETURNING does the insert and the read in a single round trip.
func (m *AlumnoModel) Create(a Alumno) (*Alumno, error) {
	query := `INSERT INTO alumnos (matricula, nombre, carrera, semestre, creditos_cursados)
	          VALUES ($1, $2, $3, $4, $5)
	          RETURNING ` + columnas

	nuevo := &Alumno{}
	err := m.DB.QueryRow(query, a.Matricula, a.Nombre, a.Carrera,
		a.Semestre, a.CreditosCursados).Scan(&nuevo.ID, &nuevo.Matricula,
		&nuevo.Nombre, &nuevo.Carrera, &nuevo.Semestre,
		&nuevo.CreditosCursados, &nuevo.Activo)
	if err != nil {
		return nil, err
	}
	return nuevo, nil
}

// Update changes the mutable fields of a student.
func (m *AlumnoModel) Update(matricula string, a Alumno) (*Alumno, error) {
	query := `UPDATE alumnos
	          SET nombre = $1, carrera = $2, semestre = $3, creditos_cursados = $4
	          WHERE matricula = $5 AND activo = TRUE
	          RETURNING ` + columnas

	actualizado := &Alumno{}
	err := m.DB.QueryRow(query, a.Nombre, a.Carrera, a.Semestre,
		a.CreditosCursados, matricula).Scan(&actualizado.ID, &actualizado.Matricula,
		&actualizado.Nombre, &actualizado.Carrera, &actualizado.Semestre,
		&actualizado.CreditosCursados, &actualizado.Activo)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("alumno no encontrado")
		}
		return nil, err
	}
	return actualizado, nil
}

// Delete is a soft delete: we flip activo to false instead of removing the
// row, because inscripciones in another database still reference this
// matricula and there is no foreign key across services to protect us.
func (m *AlumnoModel) Delete(matricula string) error {
	query := `UPDATE alumnos SET activo = FALSE WHERE matricula = $1 AND activo = TRUE`

	res, err := m.DB.Exec(query, matricula)
	if err != nil {
		return err
	}
	filas, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if filas == 0 {
		return fmt.Errorf("alumno no encontrado")
	}
	return nil
}
