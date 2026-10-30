package models

import "database/sql"

// Notificacion es un aviso ya enviado. Matricula vacia = aviso para
// coordinacion (por ejemplo, un grupo que se lleno).
type Notificacion struct {
	ID        int    `json:"id"`
	Matricula string `json:"matricula,omitempty"`
	Topico    string `json:"topico"`
	Mensaje   string `json:"mensaje"`
	CreadaEn  string `json:"creada_en"`
}

// NotificacionModel es el acceso a notificaciones_db.
type NotificacionModel struct {
	DB *sql.DB
}

// Guardar registra el aviso. Regresa false si ese evento ya se habia
// guardado: evento_id es UNIQUE, asi que un evento repetido no inserta nada,
// sin importar cual de las dos instancias lo reciba. Es la misma idea que
// eventos_procesados en materias.
func (m *NotificacionModel) Guardar(eventoID, matricula, topico, mensaje string) (bool, error) {
	res, err := m.DB.Exec(`
		INSERT INTO notificaciones (evento_id, matricula, topico, mensaje)
		VALUES ($1, NULLIF($2, ''), $3, $4)
		ON CONFLICT (evento_id) DO NOTHING`, eventoID, matricula, topico, mensaje)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func escanear(rows *sql.Rows, err error) ([]Notificacion, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lista := make([]Notificacion, 0)
	for rows.Next() {
		var n Notificacion
		if err := rows.Scan(&n.ID, &n.Matricula, &n.Topico, &n.Mensaje, &n.CreadaEn); err != nil {
			return nil, err
		}
		lista = append(lista, n)
	}
	return lista, rows.Err()
}

const columnas = `id, COALESCE(matricula, ''), topico, mensaje,
                  TO_CHAR(creada_en, 'YYYY-MM-DD HH24:MI:SS')`

// DeAlumno devuelve los avisos de un alumno, el mas nuevo primero.
func (m *NotificacionModel) DeAlumno(matricula string) ([]Notificacion, error) {
	return escanear(m.DB.Query(`SELECT `+columnas+` FROM notificaciones
	                            WHERE matricula = $1 ORDER BY id DESC LIMIT 50`, matricula))
}

// Coordinacion devuelve los avisos que no son de un alumno.
func (m *NotificacionModel) Coordinacion() ([]Notificacion, error) {
	return escanear(m.DB.Query(`SELECT ` + columnas + ` FROM notificaciones
	                            WHERE matricula IS NULL ORDER BY id DESC LIMIT 50`))
}
