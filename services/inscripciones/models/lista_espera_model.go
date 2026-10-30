package models

import (
	"database/sql"
	"errors"
	"strings"
)

var (
	ErrYaEnFila = errors.New("el alumno ya esta formado en la lista de espera de este grupo")
	ErrNoEnFila = errors.New("el alumno no esta formado en la lista de espera de este grupo")
)

// Lugar es un alumno formado en la lista de espera de un grupo.
type Lugar struct {
	ID           int    `json:"id"`
	Matricula    string `json:"matricula"`
	GrupoID      int    `json:"grupo_id"`
	MateriaClave string `json:"materia_clave"`
	Estado       string `json:"estado"`             // ESPERANDO, INSCRITO, CANCELADA o SALIO
	Motivo       string `json:"motivo,omitempty"`   // por que se cancelo
	Posicion     int    `json:"posicion,omitempty"` // 1 = el siguiente en entrar
	FormadoEn    string `json:"formado_en"`
	ResueltoEn   string `json:"resuelto_en,omitempty"`
}

// FilaModel es el acceso a la tabla lista_espera. Como el resto de models/,
// es el unico lugar donde se escribe su SQL.
//
// El orden de la fila es el id: SERIAL crece en el orden en que se insertan
// las filas, asi que el primero que se formo tiene el id mas chico.
type FilaModel struct {
	DB *sql.DB
}

const columnasFila = `id, matricula, grupo_id, materia_clave, estado, COALESCE(motivo, ''),
                      TO_CHAR(formado_en, 'YYYY-MM-DD HH24:MI:SS'),
                      COALESCE(TO_CHAR(resuelto_en, 'YYYY-MM-DD HH24:MI:SS'), '')`

func escanearLugares(rows *sql.Rows) ([]Lugar, error) {
	defer rows.Close()
	lista := make([]Lugar, 0)
	for rows.Next() {
		var l Lugar
		err := rows.Scan(&l.ID, &l.Matricula, &l.GrupoID, &l.MateriaClave, &l.Estado,
			&l.Motivo, &l.FormadoEn, &l.ResueltoEn)
		if err != nil {
			return nil, err
		}
		lista = append(lista, l)
	}
	return lista, rows.Err()
}

// Formar agrega al alumno al final de la fila del grupo.
func (m *FilaModel) Formar(matricula string, grupoID int, clave string) (*Lugar, error) {
	l := Lugar{Matricula: matricula, GrupoID: grupoID, MateriaClave: clave, Estado: "ESPERANDO"}
	err := m.DB.QueryRow(`
		INSERT INTO lista_espera (matricula, grupo_id, materia_clave)
		VALUES ($1, $2, $3)
		RETURNING id, TO_CHAR(formado_en, 'YYYY-MM-DD HH24:MI:SS')`,
		matricula, grupoID, clave).Scan(&l.ID, &l.FormadoEn)
	if err != nil {
		// El indice unico (matricula, grupo_id) WHERE estado = 'ESPERANDO'
		// es el que de verdad impide formarse dos veces, aun si dos
		// peticiones llegan al mismo tiempo a instancias distintas.
		if strings.Contains(err.Error(), "idx_espera_unica") {
			return nil, ErrYaEnFila
		}
		return nil, err
	}
	l.Posicion, err = m.posicion(l.ID, grupoID)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// posicion cuenta cuantos siguen esperando en el grupo con id <= el suyo.
func (m *FilaModel) posicion(id, grupoID int) (int, error) {
	var p int
	err := m.DB.QueryRow(`SELECT COUNT(*) FROM lista_espera
	                      WHERE grupo_id = $1 AND estado = 'ESPERANDO' AND id <= $2`,
		grupoID, id).Scan(&p)
	return p, err
}

// DeAlumno devuelve todas las veces que el alumno se formo, la mas reciente
// primero. Las que siguen esperando traen su posicion actual.
func (m *FilaModel) DeAlumno(matricula string) ([]Lugar, error) {
	rows, err := m.DB.Query(`SELECT `+columnasFila+` FROM lista_espera
	                         WHERE matricula = $1 ORDER BY id DESC`, matricula)
	if err != nil {
		return nil, err
	}
	lista, err := escanearLugares(rows)
	if err != nil {
		return nil, err
	}
	for i := range lista {
		if lista[i].Estado == "ESPERANDO" {
			lista[i].Posicion, err = m.posicion(lista[i].ID, lista[i].GrupoID)
			if err != nil {
				return nil, err
			}
		}
	}
	return lista, nil
}

// DeGrupo devuelve la fila actual del grupo, en orden.
func (m *FilaModel) DeGrupo(grupoID int) ([]Lugar, error) {
	rows, err := m.DB.Query(`SELECT `+columnasFila+` FROM lista_espera
	                         WHERE grupo_id = $1 AND estado = 'ESPERANDO' ORDER BY id`, grupoID)
	if err != nil {
		return nil, err
	}
	lista, err := escanearLugares(rows)
	for i := range lista {
		lista[i].Posicion = i + 1
	}
	return lista, err
}

// Primero devuelve al siguiente en la fila, o nil si no hay nadie.
func (m *FilaModel) Primero(grupoID int) (*Lugar, error) {
	fila, err := m.DeGrupo(grupoID)
	if err != nil || len(fila) == 0 {
		return nil, err
	}
	return &fila[0], nil
}

// Esperando dice cuantos esperan en el grupo y quien es el primero.
func (m *FilaModel) Esperando(grupoID int) (int, string, error) {
	var total int
	var primero sql.NullString
	err := m.DB.QueryRow(`
		SELECT COUNT(*),
		       (SELECT matricula FROM lista_espera
		        WHERE grupo_id = $1 AND estado = 'ESPERANDO' ORDER BY id LIMIT 1)
		FROM lista_espera WHERE grupo_id = $1 AND estado = 'ESPERANDO'`,
		grupoID).Scan(&total, &primero)
	return total, primero.String, err
}

// Resolver saca a un lugar de la fila con su estado final.
func (m *FilaModel) Resolver(id int, estado, motivo string) error {
	_, err := m.DB.Exec(`UPDATE lista_espera
	                     SET estado = $2, motivo = NULLIF($3, ''), resuelto_en = NOW()
	                     WHERE id = $1 AND estado = 'ESPERANDO'`, id, estado, motivo)
	return err
}

// MarcarInscrito resuelve la espera de un alumno que entro al grupo, si
// estaba formado. No pasa nada si no lo estaba.
func (m *FilaModel) MarcarInscrito(matricula string, grupoID int) error {
	_, err := m.DB.Exec(`UPDATE lista_espera SET estado = 'INSCRITO', resuelto_en = NOW()
	                     WHERE matricula = $1 AND grupo_id = $2 AND estado = 'ESPERANDO'`,
		matricula, grupoID)
	return err
}

// Salir es el alumno saliendose de la fila por su cuenta.
func (m *FilaModel) Salir(matricula string, grupoID int) error {
	res, err := m.DB.Exec(`UPDATE lista_espera SET estado = 'SALIO', resuelto_en = NOW()
	                       WHERE matricula = $1 AND grupo_id = $2 AND estado = 'ESPERANDO'`,
		matricula, grupoID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoEnFila
	}
	return nil
}
