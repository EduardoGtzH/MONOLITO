import { useState, useEffect } from 'react'
import './estilos.css'

// El navegador corre en TU maquina, no dentro de la red de Docker.
// Por eso aqui va localhost:8000 (nginx) y no http://nginx-gateway:8000.
const API = import.meta.env.VITE_API_URL || 'http://localhost:8000'

const DIAS = ['LUN', 'MAR', 'MIE', 'JUE', 'VIE', 'SAB']
const NOMBRE_DIA = { LUN: 'Lunes', MAR: 'Martes', MIE: 'Miércoles', JUE: 'Jueves', VIE: 'Viernes', SAB: 'Sábado' }
const HORAS = Array.from({ length: 14 }, (_, i) => 7 + i) // 7:00 a 20:00

// "07:00" -> 420. Sirve para comparar horarios y pintar el horario semanal.
const minutos = hhmm => Number(hhmm.slice(0, 2)) * 60 + Number(hhmm.slice(3, 5))
const seEmpalman = (a, b) =>
  a.dia === b.dia && minutos(a.hora_inicio) < minutos(b.hora_fin) && minutos(b.hora_inicio) < minutos(a.hora_fin)

// Los servicios responden sus errores en texto corto y sin acentos. Aqui se
// convierten en mensajes para una persona.
const MENSAJES = [
  ['el grupo ya no tiene cupo disponible', 'Este grupo ya no tiene lugares. Puedes formarte en la lista de espera.'],
  ['el alumno ya esta inscrito en este grupo', 'Ya estás inscrito en este grupo.'],
  ['el alumno ya lleva otro grupo de esta materia', 'Ya llevas otro grupo de esta materia.'],
  ['el horario se empalma con otra materia inscrita', 'El horario choca con otra de tus materias.'],
  ['el alumno ya aprobo esta materia', 'Ya aprobaste esta materia.'],
  ['falta aprobar un prerrequisito', 'Te falta aprobar un prerrequisito'],
  ['se pasa del limite de creditos del semestre', 'Excedes el límite de créditos del semestre'],
  ['hay alumnos en lista de espera para este grupo', 'Hay alumnos esperando lugar en este grupo. Fórmate en la lista de espera.'],
  ['el grupo todavia tiene lugares', 'El grupo todavía tiene lugares: inscríbete directamente.'],
  ['el periodo de inscripcion esta cerrado', 'El periodo de inscripción está cerrado'],
  ['el alumno ya esta formado en la lista de espera', 'Ya estás en la lista de espera de este grupo.'],
  ['el alumno esta dado de baja', 'Tu matrícula está dada de baja.'],
  ['un servicio remoto no respondio', 'El sistema no está disponible en este momento. Intenta de nuevo en unos segundos.'],
  ['pq: duplicate key', 'Esa matrícula ya está registrada.'],
]
function humanizar(texto) {
  for (const [clave, amable] of MENSAJES) {
    if (texto.startsWith(clave)) {
      if (amable.endsWith('.')) return amable
      const detalle = texto.slice(clave.length).replace(/^[:\s]+/, '')
        .replace('serian', 'serían').replace('cerro', 'cerró')
      return detalle ? `${amable}: ${detalle}.` : amable + '.'
    }
  }
  return texto.charAt(0).toUpperCase() + texto.slice(1)
}

const PESTANAS = [
  ['inscripcion', 'Inscripción'],
  ['horario', 'Mi horario'],
  ['avisos', 'Avisos'],
  ['historial', 'Historial'],
  ['coordinacion', 'Coordinación'],
  ['sistema', 'Sistema'],
]

export default function App() {
  const [alumnos, setAlumnos] = useState([])
  const [materias, setMaterias] = useState([])
  const [matricula, setMatricula] = useState('')
  const [alumno, setAlumno] = useState(null)        // con sus materias aprobadas
  const [inscripciones, setInscripciones] = useState([])
  const [historial, setHistorial] = useState([])
  const [fila, setFila] = useState([])
  const [avisos, setAvisos] = useState([])
  const [coordinacion, setCoordinacion] = useState([])
  const [mensaje, setMensaje] = useState(null)      // { tipo: 'ok' | 'error', texto }
  const [bitacora, setBitacora] = useState([])
  const [busqueda, setBusqueda] = useState('')
  const [pestana, setPestana] = useState('inscripcion')
  const [registrando, setRegistrando] = useState(false)
  const [nuevo, setNuevo] = useState({ matricula: '', nombre: '', carrera: 'Ingeniería en Sistemas', semestre: 1 })

  // pedir() envuelve fetch y anota en la bitacora (pestana Sistema) que
  // instancia contesto, con el header X-Instance-Id. Las consultas
  // automaticas de cada pocos segundos no se anotan.
  async function pedir(ruta, opciones = {}, anotar = true) {
    const resp = await fetch(API + ruta, {
      ...opciones,
      headers: { 'Content-Type': 'application/json', ...(opciones.headers || {}) },
    })
    if (anotar) {
      const instancia = resp.headers.get('X-Instance-Id') || '—'
      setBitacora(b => [
        { hora: new Date().toLocaleTimeString(), metodo: opciones.method || 'GET', ruta, instancia, status: resp.status },
        ...b,
      ].slice(0, 30))
    }
    const texto = await resp.text()
    if (!resp.ok) throw new Error(humanizar(texto.trim() || 'error ' + resp.status))
    return texto ? JSON.parse(texto) : null
  }

  const avisar = (tipo, texto) => setMensaje({ tipo, texto })

  async function cargarCatalogo(anotar = true) {
    try {
      const [a, m, c] = await Promise.all([
        pedir('/alumnos', {}, anotar),
        pedir('/materias', {}, anotar),
        pedir('/notificaciones/coordinacion', {}, false).catch(() => []),
      ])
      setAlumnos(a); setMaterias(m); setCoordinacion(c)
    } catch {
      avisar('error', 'No pudimos cargar la oferta académica. Intenta de nuevo en unos segundos.')
    }
  }

  // Todo lo del alumno elegido. Se llama al elegirlo, despues de cada accion
  // y cada 4 segundos, para que aparezcan solos los avisos y la entrada
  // desde la lista de espera (que llegan por Kafka).
  async function cargarAlumno(m, anotar = true) {
    if (!m) return
    try {
      const [a, ins, hist, f, av] = await Promise.all([
        pedir('/alumnos/' + m, {}, anotar),
        pedir('/inscripciones?matricula=' + m, {}, anotar),
        pedir('/inscripciones?historial=1&matricula=' + m, {}, false),
        pedir('/lista-espera?matricula=' + m, {}, false),
        pedir('/notificaciones?matricula=' + m, {}, false).catch(() => []),
      ])
      setAlumno(a); setInscripciones(ins); setHistorial(hist); setFila(f); setAvisos(av)
    } catch (e) { avisar('error', e.message) }
  }

  useEffect(() => { cargarCatalogo() }, [])

  useEffect(() => {
    if (!matricula) { setAlumno(null); return }
    cargarAlumno(matricula)
    const reloj = setInterval(() => { cargarAlumno(matricula, false); cargarCatalogo(false) }, 4000)
    return () => clearInterval(reloj)
  }, [matricula])

  // Despues de una accion: recargar ya, y otra vez en un segundo, porque la
  // devolucion de un lugar llega por Kafka un instante despues.
  async function refrescar() {
    await Promise.all([cargarCatalogo(false), cargarAlumno(matricula, false)])
    setTimeout(() => { cargarCatalogo(false); cargarAlumno(matricula, false) }, 1200)
  }

  async function accion(texto, fn) {
    setMensaje(null)
    try { await fn(); avisar('ok', texto); await refrescar() }
    catch (e) { avisar('error', e.message) }
  }

  const nombreGrupo = g => `${g.materia?.nombre || g.materia_clave} (grupo ${g.numero})`

  const inscribir = g => accion(`Quedaste inscrito en ${nombreGrupo(g)}.`, () =>
    pedir('/inscripciones', { method: 'POST', body: JSON.stringify({ matricula, grupo_id: g.id }) }))

  const formarse = g => accion(`Estás en la lista de espera de ${nombreGrupo(g)}. Te avisaremos si se libera un lugar.`, () =>
    pedir('/lista-espera', { method: 'POST', body: JSON.stringify({ matricula, grupo_id: g.id }) }))

  function darDeBaja(g) {
    if (!window.confirm(`¿Seguro que quieres dar de baja ${nombreGrupo(g)}?`)) return
    accion(`Diste de baja ${nombreGrupo(g)}.`, () =>
      pedir(`/inscripciones/${matricula}/${g.id}`, { method: 'DELETE' }))
  }

  const salirDeFila = (l, g) => accion(`Saliste de la lista de espera de ${g ? nombreGrupo(g) : l.materia_clave}.`, () =>
    pedir(`/lista-espera/${matricula}/${l.grupo_id}`, { method: 'DELETE' }))

  async function registrar(e) {
    e.preventDefault()
    await accion(`Bienvenido(a), ${nuevo.nombre}. Tu matrícula ${nuevo.matricula} quedó registrada.`, () =>
      pedir('/alumnos', { method: 'POST', body: JSON.stringify({ ...nuevo, semestre: Number(nuevo.semestre) }) }))
    setMatricula(nuevo.matricula)
    setRegistrando(false)
  }

  // ------------------------------------------------------------ datos derivados
  const grupos = materias.flatMap(m => m.grupos.map(g => ({ ...g, materia: m })))
  const porId = Object.fromEntries(grupos.map(g => [g.id, g]))
  const mios = inscripciones.map(i => porId[i.grupo_id]).filter(Boolean)
  const enFila = fila.filter(l => l.estado === 'ESPERANDO')
  const creditos = mios.reduce((s, g) => s + g.materia.creditos, 0)
  const aprobadas = alumno?.aprobadas || []
  const nombreCorto = alumno ? alumno.nombre.split(' ')[0] : ''

  // Que puede hacer el alumno con un grupo. Es solo una ayuda visual: quien
  // decide de verdad es la cadena de validaciones del servidor.
  function estadoDe(g) {
    if (mios.some(x => x.id === g.id)) return { texto: 'Inscrito', marca: 'si' }
    const f = enFila.find(l => l.grupo_id === g.id)
    if (f) return { texto: `En espera, lugar ${f.posicion}`, marca: 'fila' }
    if (aprobadas.includes(g.materia_clave)) return { texto: 'Ya la aprobaste' }
    const faltan = g.materia.requiere.filter(r => !aprobadas.includes(r))
    if (faltan.length) return { texto: 'Requiere ' + faltan.join(', ') }
    if (mios.some(x => x.materia_clave === g.materia_clave)) return { texto: 'Ya llevas esta materia' }
    const choque = mios.find(x => seEmpalman(x, g))
    if (choque) return { texto: 'Choca con ' + choque.materia_clave }
    if (g.cupo_ocupado >= g.cupo_maximo) return { boton: 'Lista de espera', hacer: formarse, secundario: true }
    return { boton: 'Inscribirme', hacer: inscribir }
  }

  const filtro = busqueda.trim().toLowerCase()
  const visibles = materias.filter(m =>
    !filtro || m.clave.toLowerCase().includes(filtro) || m.nombre.toLowerCase().includes(filtro))

  // Celdas del horario: un grupo ocupa tantas filas como horas dura.
  function celda(dia, hora) {
    const g = mios.find(x => x.dia === dia && minutos(x.hora_inicio) <= hora * 60 && hora * 60 < minutos(x.hora_fin))
    if (!g) return <td key={dia}></td>
    if (minutos(g.hora_inicio) !== hora * 60 && hora !== HORAS[0]) return null
    const filas = Math.ceil((minutos(g.hora_fin) - Math.max(minutos(g.hora_inicio), hora * 60)) / 60)
    return (
      <td key={dia} rowSpan={filas} className="clase">
        <strong>{g.materia.nombre}</strong>
        <span>{g.hora_inicio} – {g.hora_fin}</span>
        <span>{g.profesor}</span>
      </td>
    )
  }

  // ------------------------------------------------------------ pantalla
  return (
    <>
      <header className="encabezado">
        <div className="contenido encabezado-fila">
          <div className="marca">
            <span className="marca-titulo">Inscripciones</span>
            <span className="marca-sub">Servicios Escolares</span>
          </div>
          <div className="sesion">
            <select value={matricula} onChange={e => { setMatricula(e.target.value); setMensaje(null) }}>
              <option value="">Selecciona tu matrícula</option>
              {alumnos.map(a => (
                <option key={a.matricula} value={a.matricula}>{a.matricula} · {a.nombre}</option>
              ))}
            </select>
            <button className="enlace-claro" onClick={() => setRegistrando(r => !r)}>
              {registrando ? 'Cancelar' : 'Nuevo alumno'}
            </button>
          </div>
        </div>
      </header>

      <nav className="pestanas">
        <div className="contenido">
          {PESTANAS.map(([id, texto]) => (
            <button key={id} className={pestana === id ? 'activa' : ''} onClick={() => setPestana(id)}>
              {texto}
              {id === 'avisos' && avisos.length > 0 && <span className="globo">{avisos.length}</span>}
            </button>
          ))}
        </div>
      </nav>

      <div className="contenido cuerpo">
        {registrando && (
          <form className="registro" onSubmit={registrar}>
            <h2>Registro de alumno</h2>
            <div className="campos">
              <label>Matrícula<input required value={nuevo.matricula}
                onChange={e => setNuevo({ ...nuevo, matricula: e.target.value })} /></label>
              <label>Nombre completo<input required value={nuevo.nombre}
                onChange={e => setNuevo({ ...nuevo, nombre: e.target.value })} /></label>
              <label>Carrera<select value={nuevo.carrera} onChange={e => setNuevo({ ...nuevo, carrera: e.target.value })}>
                <option>Ingeniería en Sistemas</option>
                <option>Ingeniería Industrial</option>
                <option>Ingeniería Mecatrónica</option>
              </select></label>
              <label>Semestre<input type="number" min="1" max="12" value={nuevo.semestre}
                onChange={e => setNuevo({ ...nuevo, semestre: e.target.value })} /></label>
            </div>
            <button>Registrar</button>
          </form>
        )}

        {mensaje && <p className={'mensaje ' + mensaje.tipo}>{mensaje.texto}</p>}

        {alumno && pestana !== 'coordinacion' && pestana !== 'sistema' && (
          <div className="saludo">
            <h1>Hola, {nombreCorto}</h1>
            <p>{alumno.carrera} · {alumno.semestre}.º semestre · Matrícula {alumno.matricula}</p>
          </div>
        )}

        {!alumno && pestana !== 'coordinacion' && pestana !== 'sistema' && (
          <div className="bienvenida">
            <h1>Inscripción de materias</h1>
            <p>Selecciona tu matrícula en la parte superior para consultar la oferta, armar tu horario y revisar tus avisos.</p>
          </div>
        )}

        {/* ---------------------------------------------------- Inscripcion */}
        {pestana === 'inscripcion' && alumno && (
          <div className="dos-columnas">
            <section>
              <div className="titulo-seccion">
                <h2>Oferta académica</h2>
                <input className="buscar" placeholder="Buscar materia" value={busqueda}
                  onChange={e => setBusqueda(e.target.value)} />
              </div>
              {visibles.map(m => (
                <article key={m.id} className="materia">
                  <header>
                    <h3>{m.nombre}</h3>
                    <span className="detalle">
                      {m.clave} · {m.creditos} créditos{m.requiere.length > 0 && ' · Requiere ' + m.requiere.join(', ')}
                    </span>
                  </header>
                  <div className="desliza">
                    <table className="grupos">
                      <tbody>
                        {m.grupos.map(grupo => {
                          const g = { ...grupo, materia: m }
                          const est = estadoDe(g)
                          const lleno = g.cupo_ocupado >= g.cupo_maximo
                          return (
                            <tr key={g.id}>
                              <td className="angosta">Grupo {g.numero}</td>
                              <td>{g.profesor}</td>
                              <td className="angosta">{NOMBRE_DIA[g.dia]} {g.hora_inicio}–{g.hora_fin}</td>
                              <td className={'angosta lugares ' + (lleno ? 'lleno' : '')}>
                                {lleno ? 'Sin lugares' : g.cupo_maximo - g.cupo_ocupado === 1 ? '1 lugar' : `${g.cupo_maximo - g.cupo_ocupado} lugares`}
                              </td>
                              <td className="accion">
                                {est.boton
                                  ? <button className={est.secundario ? 'secundario' : ''} onClick={() => est.hacer(g)}>{est.boton}</button>
                                  : <span className={'estado ' + (est.marca || '')}>{est.texto}</span>}
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                </article>
              ))}
            </section>

            <aside>
              <section className="panel">
                <h2>Mis materias</h2>
                {mios.length === 0 && <p className="vacio">Aún no tienes materias inscritas.</p>}
                <ul className="lista">
                  {mios.map(g => (
                    <li key={g.id}>
                      <div>
                        <strong>{g.materia.nombre}</strong>
                        <span>Grupo {g.numero} · {NOMBRE_DIA[g.dia]} {g.hora_inicio}–{g.hora_fin}</span>
                      </div>
                      <button className="enlace" onClick={() => darDeBaja(g)}>Dar de baja</button>
                    </li>
                  ))}
                </ul>
                {mios.length > 0 && <p className="total">Total: {creditos} créditos</p>}
              </section>

              <section className="panel">
                <h2>Lista de espera</h2>
                {fila.length === 0 && <p className="vacio">No estás en ninguna lista de espera.</p>}
                <ul className="lista">
                  {fila.slice(0, 6).map(l => {
                    const g = porId[l.grupo_id]
                    return (
                      <li key={l.id} className={l.estado === 'ESPERANDO' ? '' : 'resuelta'}>
                        <div>
                          <strong>{g?.materia.nombre || l.materia_clave}</strong>
                          <span>
                            {l.estado === 'ESPERANDO' && `Grupo ${g?.numero} · Lugar ${l.posicion} en la fila`}
                            {l.estado === 'INSCRITO' && 'Te inscribimos en cuanto se liberó un lugar'}
                            {l.estado === 'CANCELADA' && 'Ya no se pudo inscribir: ' + humanizar(l.motivo || '')}
                            {l.estado === 'SALIO' && 'Saliste de la fila'}
                          </span>
                        </div>
                        {l.estado === 'ESPERANDO' && <button className="enlace" onClick={() => salirDeFila(l, g)}>Salir</button>}
                      </li>
                    )
                  })}
                </ul>
              </section>
            </aside>
          </div>
        )}

        {/* ---------------------------------------------------- Horario */}
        {pestana === 'horario' && alumno && (
          <section>
            <div className="titulo-seccion">
              <h2>Mi horario</h2>
              <span className="detalle">{mios.length} materias · {creditos} créditos</span>
            </div>
            <div className="desliza">
              <table className="horario">
                <thead><tr><th></th>{DIAS.map(d => <th key={d}>{NOMBRE_DIA[d]}</th>)}</tr></thead>
                <tbody>
                  {HORAS.map(h => (
                    <tr key={h}>
                      <th>{String(h).padStart(2, '0')}:00</th>
                      {DIAS.map(d => celda(d, h))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {/* ---------------------------------------------------- Avisos */}
        {pestana === 'avisos' && alumno && (
          <section>
            <h2>Avisos</h2>
            {avisos.length === 0 && <p className="vacio">No tienes avisos.</p>}
            <ul className="avisos">
              {avisos.map(a => (
                <li key={a.id}><p>{a.mensaje}</p><time>{a.creada_en}</time></li>
              ))}
            </ul>
          </section>
        )}

        {/* ---------------------------------------------------- Historial */}
        {pestana === 'historial' && alumno && (
          <section>
            <h2>Historial de inscripciones</h2>
            {historial.length === 0 && <p className="vacio">Todavía no hay movimientos.</p>}
            {historial.length > 0 && (
              <div className="desliza">
                <table className="tabla">
                  <thead><tr><th>Materia</th><th>Grupo</th><th>Inscripción</th><th>Estado</th></tr></thead>
                  <tbody>
                    {historial.map(i => {
                      const g = porId[i.grupo_id]
                      return (
                        <tr key={i.id}>
                          <td>{g?.materia.nombre || i.materia_clave}</td>
                          <td>{g?.numero || i.grupo_id}</td>
                          <td>{i.inscrito_en}</td>
                          <td>{i.estado === 'ACTIVA' ? 'Inscrita' : 'Baja el ' + i.baja_en}</td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

        {/* ---------------------------------------------------- Coordinacion */}
        {pestana === 'coordinacion' && (
          <div className="dos-columnas">
            <section>
              <h2>Ocupación de grupos</h2>
              <div className="desliza">
                <table className="tabla">
                  <thead><tr><th>Materia</th><th>Grupo</th><th>Horario</th><th>Ocupación</th></tr></thead>
                  <tbody>
                    {grupos.map(g => (
                      <tr key={g.id}>
                        <td>{g.materia.nombre}</td>
                        <td>{g.numero}</td>
                        <td>{NOMBRE_DIA[g.dia]} {g.hora_inicio}–{g.hora_fin}</td>
                        <td>
                          <div className="barra-cupo"><span style={{ width: `${(100 * g.cupo_ocupado) / g.cupo_maximo}%` }} /></div>
                          {g.cupo_ocupado} de {g.cupo_maximo}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
            <aside>
              <section className="panel">
                <h2>Grupos llenos</h2>
                {coordinacion.length === 0 && <p className="vacio">Ningún grupo se ha llenado.</p>}
                <ul className="avisos">
                  {coordinacion.map(a => <li key={a.id}><p>{a.mensaje}</p><time>{a.creada_en}</time></li>)}
                </ul>
              </section>
            </aside>
          </div>
        )}

        {/* ---------------------------------------------------- Sistema */}
        {pestana === 'sistema' && (
          <section>
            <h2>Peticiones recientes</h2>
            {bitacora.length === 0 && <p className="vacio">Sin peticiones todavía.</p>}
            {bitacora.length > 0 && (
              <div className="desliza">
                <table className="tabla">
                  <thead><tr><th>Hora</th><th>Petición</th><th>Atendió</th><th>Estado</th></tr></thead>
                  <tbody>
                    {bitacora.map((b, i) => (
                      <tr key={i}>
                        <td>{b.hora}</td>
                        <td><code>{b.metodo} {b.ruta}</code></td>
                        <td><strong>{b.instancia}</strong></td>
                        <td>{b.status}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}
      </div>

      <footer className="pie">
        <div className="contenido">Servicios Escolares · Inscripción de materias</div>
      </footer>
    </>
  )
}
