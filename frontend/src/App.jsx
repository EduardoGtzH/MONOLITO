import { useState, useEffect } from 'react'

// El navegador corre en TU maquina, no dentro de la red de Docker.
// Por eso aqui va localhost:8000 y no http://loadbalancer:8000.
const API = import.meta.env.VITE_API_URL || 'http://localhost:8000'

export default function App() {
  const [alumnos, setAlumnos] = useState([])
  const [materias, setMaterias] = useState([])
  const [inscripciones, setInscripciones] = useState([])

  const [matricula, setMatricula] = useState('')
  const [grupoId, setGrupoId] = useState('')

  const [mensaje, setMensaje] = useState('')
  const [bitacora, setBitacora] = useState([])

  // pedir() envuelve fetch y guarda quien contesto. El header X-Instance-Id
  // lo pone cada servicio; X-Backend-Target lo pone el load balancer.
  // Para que el navegador pueda leerlos, el load balancer tiene que
  // exponerlos con Access-Control-Expose-Headers.
  async function pedir(ruta, opciones = {}) {
    const resp = await fetch(API + ruta, {
      ...opciones,
      headers: { 'Content-Type': 'application/json', ...(opciones.headers || {}) },
    })

    const instancia = resp.headers.get('X-Instance-Id') || '?'
    const servicio = resp.headers.get('X-Backend-Service') || '?'

    setBitacora(b => [
      { hora: new Date().toLocaleTimeString(), ruta, servicio, instancia, status: resp.status },
      ...b,
    ].slice(0, 12))

    const texto = await resp.text()
    if (!resp.ok) throw new Error(texto || ('error ' + resp.status))
    return texto ? JSON.parse(texto) : null
  }

  async function cargarAlumnos() {
    try { setAlumnos(await pedir('/alumnos')) }
    catch (e) { setMensaje('Error: ' + e.message) }
  }

  async function cargarMaterias() {
    try { setMaterias(await pedir('/materias')) }
    catch (e) { setMensaje('Error: ' + e.message) }
  }

  async function cargarInscripciones(m) {
    if (!m) return
    try { setInscripciones(await pedir('/inscripciones?matricula=' + m)) }
    catch (e) { setMensaje('Error: ' + e.message) }
  }

  async function inscribir() {
    setMensaje('')
    try {
      const r = await pedir('/inscripciones', {
        method: 'POST',
        body: JSON.stringify({ matricula, grupo_id: Number(grupoId) }),
      })
      setMensaje('Inscrito en ' + r.materia_clave)
      await cargarMaterias()
      await cargarInscripciones(matricula)
    } catch (e) {
      setMensaje('Rechazado: ' + e.message)
    }
  }

  async function darDeBaja(gid) {
    setMensaje('')
    try {
      await pedir('/inscripciones/' + matricula + '/' + gid, { method: 'DELETE' })
      setMensaje('Baja registrada')
      await cargarMaterias()
      await cargarInscripciones(matricula)
    } catch (e) {
      setMensaje('Error: ' + e.message)
    }
  }

  useEffect(() => { cargarAlumnos(); cargarMaterias() }, [])

  return (
    <div>
      <h1>Inscripción de Materias</h1>
      <p>API: {API}</p>
      {mensaje && <p><strong>{mensaje}</strong></p>}

      <hr />
      <h2>Inscribir</h2>
      <p>
        <label>Alumno: </label>
        <select value={matricula} onChange={e => { setMatricula(e.target.value); cargarInscripciones(e.target.value) }}>
          <option value="">-- selecciona --</option>
          {alumnos.map(a => (
            <option key={a.matricula} value={a.matricula}>{a.matricula} — {a.nombre}</option>
          ))}
        </select>
      </p>
      <p>
        <label>Grupo: </label>
        <select value={grupoId} onChange={e => setGrupoId(e.target.value)}>
          <option value="">-- selecciona --</option>
          {materias.flatMap(m => m.grupos.map(g => (
            <option key={g.id} value={g.id}>
              {m.clave} gpo {g.numero} — {g.dia} {g.hora_inicio}-{g.hora_fin} — {g.cupo_ocupado}/{g.cupo_maximo}
            </option>
          )))}
        </select>
      </p>
      <p>
        <button onClick={inscribir} disabled={!matricula || !grupoId}>Inscribir</button>
      </p>

      <hr />
      <h2>Inscripciones de {matricula || '...'}</h2>
      <ul>
        {inscripciones.map(i => (
          <li key={i.id}>
            {i.materia_clave} (grupo id {i.grupo_id}) — {i.inscrito_en}
            {' '}<button onClick={() => darDeBaja(i.grupo_id)}>Dar de baja</button>
          </li>
        ))}
      </ul>

      <hr />
      <h2>Catálogo <button onClick={cargarMaterias}>Recargar</button></h2>
      <table>
        <thead>
          <tr><th>Materia</th><th>Gpo</th><th>Profesor</th><th>Horario</th><th>Cupo</th></tr>
        </thead>
        <tbody>
          {materias.flatMap(m => m.grupos.map(g => (
            <tr key={g.id}>
              <td>{m.clave} {m.nombre}</td>
              <td>{g.numero}</td>
              <td>{g.profesor}</td>
              <td>{g.dia} {g.hora_inicio}-{g.hora_fin}</td>
              <td>{g.cupo_ocupado}/{g.cupo_maximo}</td>
            </tr>
          )))}
        </tbody>
      </table>

      <hr />
      <h2>Bitácora de instancias</h2>
      <p>Quién respondió cada petición. Aquí se ve el round-robin.</p>
      <table>
        <thead>
          <tr><th>Hora</th><th>Ruta</th><th>Servicio</th><th>Instancia</th><th>Status</th></tr>
        </thead>
        <tbody>
          {bitacora.map((b, i) => (
            <tr key={i}>
              <td>{b.hora}</td><td>{b.ruta}</td><td>{b.servicio}</td>
              <td><strong>{b.instancia}</strong></td><td>{b.status}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
