package models

import (
	"database/sql"
	"errors"
)

// Sentinel errors let the controller map a failure to the right HTTP status
// without parsing error strings.
var (
	ErrGrupoNoEncontrado   = errors.New("grupo no encontrado")
	ErrMateriaNoEncontrada = errors.New("materia no encontrada")
	ErrSinCupo             = errors.New("el grupo ya no tiene cupo disponible")
)

// Grupo is one section of a course: its own schedule, professor and seats.
type Grupo struct {
	ID           int    `json:"id"`
	MateriaID    int    `json:"materia_id"`
	MateriaClave string `json:"materia_clave"`
	Numero       string `json:"numero"`
	Profesor     string `json:"profesor"`
	Dia          string `json:"dia"`
	HoraInicio   string `json:"hora_inicio"`
	HoraFin      string `json:"hora_fin"`
	CupoMaximo   int    `json:"cupo_maximo"`
	CupoOcupado  int    `json:"cupo_ocupado"`
}

// Materia is the course itself; the seats live in its grupos.
type Materia struct {
	ID       int     `json:"id"`
	Clave    string  `json:"clave"`
	Nombre   string  `json:"nombre"`
	Creditos int     `json:"creditos"`
	Carrera  string  `json:"carrera"`
	Grupos   []Grupo `json:"grupos"`
}

type MateriaModel struct {
	DB *sql.DB
}

// TO_CHAR converts the TIME columns to plain strings in the query itself.
// Postgres TIME values would otherwise scan into an awkward type; this way
// they arrive as "07:00" and go straight into JSON.
const queryCatalogo = `
	SELECT m.id, m.clave, m.nombre, m.creditos, m.carrera,
	       g.id, g.numero, g.profesor, g.dia,
	       TO_CHAR(g.hora_inicio, 'HH24:MI'),
	       TO_CHAR(g.hora_fin, 'HH24:MI'),
	       g.cupo_maximo, g.cupo_ocupado
	FROM materias m
	LEFT JOIN grupos g ON g.materia_id = m.id`

// GetCatalogo returns every course with its sections. The JOIN gives one row
// per grupo, so we fold them back into materias as we scan.
func (m *MateriaModel) GetCatalogo() ([]Materia, error) {
	rows, err := m.DB.Query(queryCatalogo + ` ORDER BY m.clave, g.numero`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return escanearCatalogo(rows)
}

// GetByClave returns a single course with its sections.
func (m *MateriaModel) GetByClave(clave string) (*Materia, error) {
	rows, err := m.DB.Query(queryCatalogo+` WHERE m.clave = $1 ORDER BY g.numero`, clave)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	materias, err := escanearCatalogo(rows)
	if err != nil {
		return nil, err
	}
	if len(materias) == 0 {
		return nil, ErrMateriaNoEncontrada
	}
	return &materias[0], nil
}

// escanearCatalogo collapses the JOIN result into a slice of materias.
// A LEFT JOIN produces NULL grupo columns for a course with no sections,
// so every grupo field is scanned into a nullable type first.
func escanearCatalogo(rows *sql.Rows) ([]Materia, error) {
	materias := make([]Materia, 0)
	indice := make(map[int]int) // materia id -> position in the slice

	for rows.Next() {
		var mat Materia
		var gID, gCupoMax, gCupoOcu sql.NullInt64
		var gNum, gProf, gDia, gIni, gFin sql.NullString

		err := rows.Scan(&mat.ID, &mat.Clave, &mat.Nombre, &mat.Creditos, &mat.Carrera,
			&gID, &gNum, &gProf, &gDia, &gIni, &gFin, &gCupoMax, &gCupoOcu)
		if err != nil {
			return nil, err
		}

		pos, vista := indice[mat.ID]
		if !vista {
			mat.Grupos = make([]Grupo, 0)
			materias = append(materias, mat)
			pos = len(materias) - 1
			indice[mat.ID] = pos
		}

		if gID.Valid {
			materias[pos].Grupos = append(materias[pos].Grupos, Grupo{
				ID:           int(gID.Int64),
				MateriaID:    mat.ID,
				MateriaClave: mat.Clave,
				Numero:       gNum.String,
				Profesor:     gProf.String,
				Dia:          gDia.String,
				HoraInicio:   gIni.String,
				HoraFin:      gFin.String,
				CupoMaximo:   int(gCupoMax.Int64),
				CupoOcupado:  int(gCupoOcu.Int64),
			})
		}
	}
	return materias, rows.Err()
}

// GetGrupo returns one section. The inscripciones service calls this over
// HTTP to check the schedule and the seat count before enrolling.
func (m *MateriaModel) GetGrupo(id int) (*Grupo, error) {
	g := &Grupo{}
	query := `
		SELECT g.id, g.materia_id, m.clave, g.numero, g.profesor, g.dia,
		       TO_CHAR(g.hora_inicio, 'HH24:MI'),
		       TO_CHAR(g.hora_fin, 'HH24:MI'),
		       g.cupo_maximo, g.cupo_ocupado
		FROM grupos g JOIN materias m ON m.id = g.materia_id
		WHERE g.id = $1`

	err := m.DB.QueryRow(query, id).Scan(&g.ID, &g.MateriaID, &g.MateriaClave,
		&g.Numero, &g.Profesor, &g.Dia, &g.HoraInicio, &g.HoraFin,
		&g.CupoMaximo, &g.CupoOcupado)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrGrupoNoEncontrado
		}
		return nil, err
	}
	return g, nil
}

// ApartarLugar claims one seat. Checking the seat count and incrementing it
// are two separate statements, so two concurrent enrollments could both read
// "29 of 30" and both write 30 — overbooking the group. SELECT ... FOR UPDATE
// locks the row for the duration of the transaction, forcing the second
// request to wait and re-read the updated count.
func (m *MateriaModel) ApartarLugar(grupoID int) (*Grupo, error) {
	tx, err := m.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // no-op once Commit succeeds

	var ocupado, maximo int
	err = tx.QueryRow(
		`SELECT cupo_ocupado, cupo_maximo FROM grupos WHERE id = $1 FOR UPDATE`,
		grupoID).Scan(&ocupado, &maximo)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrGrupoNoEncontrado
		}
		return nil, err
	}

	if ocupado >= maximo {
		return nil, ErrSinCupo
	}

	_, err = tx.Exec(`UPDATE grupos SET cupo_ocupado = cupo_ocupado + 1 WHERE id = $1`, grupoID)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m.GetGrupo(grupoID)
}

// LiberarLugar gives a seat back when a student drops the group. GREATEST
// keeps the counter from going negative if this is called twice.
func (m *MateriaModel) LiberarLugar(grupoID int) (*Grupo, error) {
	res, err := m.DB.Exec(
		`UPDATE grupos SET cupo_ocupado = GREATEST(cupo_ocupado - 1, 0) WHERE id = $1`,
		grupoID)
	if err != nil {
		return nil, err
	}
	filas, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if filas == 0 {
		return nil, ErrGrupoNoEncontrado
	}
	return m.GetGrupo(grupoID)
}
