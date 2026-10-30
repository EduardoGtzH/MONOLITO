package models

import (
	"database/sql"
	"fmt"
)

// Alumno is the resource this service owns. The json tags control how it
// is serialized in the API responses.
type Alumno struct {
	ID               int    `json:"id"`
	Matricula        string `json:"matricula"`
	Nombre           string `json:"nombre"`
	Carrera          string `json:"carrera"`
	Semestre         int    `json:"semestre"`
	CreditosCursados int    `json:"creditos_cursados"`
	Activo           bool   `json:"activo"`

	// Claves de las materias que ya aprobo. Solo viene al pedir un alumno
	// (GET /alumnos/{matricula}): inscripciones la usa para los prerrequisitos.
	Aprobadas []string `json:"aprobadas,omitempty"`
}

// AlumnoModel is the M in MVC: it holds the database handle and is the only
// place in this service where SQL is written. Controllers never touch the DB.
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

	alumnos := make([]Alumno, 0)
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

// GetByMatricula is the endpoint the inscripciones service calls over HTTP
// to verify a student exists before enrolling them.
func (m *AlumnoModel) GetByMatricula(matricula string) (*Alumno, error) {
	alumno := &Alumno{}
	query := `SELECT ` + columnas + ` FROM alumnos WHERE matricula = $1`

	err := m.DB.QueryRow(query, matricula).Scan(&alumno.ID, &alumno.Matricula,
		&alumno.Nombre, &alumno.Carrera, &alumno.Semestre,
		&alumno.CreditosCursados, &alumno.Activo)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("alumno %s no encontrado", matricula)
		}
		return nil, err
	}

	alumno.Aprobadas, err = m.materiasAprobadas(matricula)
	if err != nil {
		return nil, err
	}
	return alumno, nil
}

// materiasAprobadas lee el historial academico del alumno.
func (m *AlumnoModel) materiasAprobadas(matricula string) ([]string, error) {
	rows, err := m.DB.Query(`SELECT materia_clave FROM materias_aprobadas
	                         WHERE matricula = $1 ORDER BY materia_clave`, matricula)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	claves := make([]string, 0)
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		claves = append(claves, c)
	}
	return claves, rows.Err()
}

// Create inserts a new student. RETURNING lets Postgres hand back the
// generated id in the same round trip instead of a second SELECT.
func (m *AlumnoModel) Create(a Alumno) (*Alumno, error) {
	query := `INSERT INTO alumnos (matricula, nombre, carrera, semestre, creditos_cursados)
	          VALUES ($1, $2, $3, $4, $5)
	          RETURNING id, activo`

	err := m.DB.QueryRow(query, a.Matricula, a.Nombre, a.Carrera,
		a.Semestre, a.CreditosCursados).Scan(&a.ID, &a.Activo)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
