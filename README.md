# Sistema Distribuido de Inscripción de Materias

Proyecto final de Cómputo Distribuido. Sistema de inscripción UP

**13 contenedores:** 1 frontend, 1 load balancer, 1 middleware, 9 instancias de servicio
(3 por cada uno) y 1 base de datos.

---

## Arquitectura

```
                      ┌──────────────┐
                      │   Frontend   │  React + Vite  :3000
                      └──────┬───────┘
                             │  HTTP
                      ┌──────▼───────┐           ┌──────────────────┐
                      │ Load Balancer│ ········> │    Middleware     │  :8500
                      │    :8000     │  resolve  │ service discovery │
                      └──────┬───────┘ <········ │  health monitor   │
                             │                   └─────────┬────────┘
            ┌────────────────┼────────────────┐            │
            │                │                │            │ heartbeat
    ┌───────▼──────┐ ┌───────▼──────┐ ┌───────▼──────┐     │ cada 5s
    │   alumnos    │ │   materias   │ │inscripciones │ <···┘
    │   x3 (:8080) │ │   x3 (:8080) │ │   x3 (:8080) │
    └───────┬──────┘ └───────┬──────┘ └───────┬──────┘
            │                │                │
            └────────────────┼────────────────┘
                      ┌──────▼───────┐
                      │  PostgreSQL  │  3 bases separadas  :5432
                      └──────────────┘
```

Las líneas continuas son tráfico de usuario. Las punteadas son metadatos.

El **middleware** es el plano de control: mantiene el registro de servicios y
monitorea salud, pero ninguna petición de usuario pasa por él. El **load balancer**
es el plano de datos: está en el camino crítico de cada petición y solo decide a
qué instancia sana le toca. Es la misma separación que hacen Consul/Envoy o el
API server y kube-proxy en Kubernetes.

---

## Requisitos

- Linux (probado en Kali Linux rolling)
- Docker Engine 24+ y el plugin Docker Compose v2
- Puertos libres: 3000, 5432, 8000, 8081-8083, 8091-8093, 8101-8103, 8500

El script de instalación instala Docker automáticamente si no está presente.

---

## Instalación y ejecución

```bash
git clone https://github.com/EduardoGtzH/MONOLITO/
cd MONOLITO
chmod +x setup.sh demo.sh
./setup.sh
```

El script instala dependencias, genera el archivo `.env`, construye las imágenes,
levanta los 13 contenedores y espera a que el sistema reporte las 9 instancias sanas.
La primera ejecución tarda varios minutos compilando.

Para reconstruir desde cero borrando los datos:

```bash
./setup.sh --limpio
```

Una vez arriba:

| Recurso | URL |
|---|---|
| Frontend | http://localhost:3000 |
| Load balancer | http://localhost:8000 |
| Estado del registro | http://localhost:8500/status |

Para apagar todo: `docker compose down`

---

## Estructura del proyecto

```
.
├── db/init/                  Scripts SQL de inicialización
│   ├── 01-crear-bases.sql
│   ├── 02-alumnos.sql
│   ├── 03-materias.sql
│   └── 04-inscripciones.sql
├── services/
│   ├── alumnos/              Servicio de estudiantes
│   ├── materias/             Catálogo, grupos y cupos
│   └── inscripciones/        Orquestación e inscripción
├── middleware/               Service discovery + health monitor
│   ├── registro.go
│   ├── monitor.go
│   └── routes.json           Documento de service discovery
├── loadbalancer/             Reverse proxy con round robin
│   └── balanceador.go
├── frontend/                 React + Vite
├── docker-compose.yml
├── setup.sh                  Script de automatización
└── demo.sh                   Script de pruebas en vivo
```

Cada servicio sigue la misma estructura MVC:

```
servicio/
├── models/          Structs y acceso a datos. Único lugar con SQL.
├── controllers/     Handlers HTTP. Parsean, delegan, serializan JSON.
├── connectors/      Clientes HTTP a otros servicios (solo inscripciones)
├── main.go          Inyección de dependencias y ruteo
└── Dockerfile       Build multi-etapa: golang-alpine → alpine
```

---

## API

Todas las rutas se consumen a través del load balancer en el puerto 8000.

### Alumnos

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/alumnos` | Lista de alumnos activos |
| GET | `/alumnos/{matricula}` | Un alumno |
| POST | `/alumnos` | Registra un alumno |

### Materias

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/materias` | Catálogo con grupos anidados |
| GET | `/materias/{clave}` | Una materia con sus grupos |
| GET | `/grupos/{id}` | Un grupo |
| POST | `/grupos/{id}/apartar` | Aparta un lugar |
| POST | `/grupos/{id}/liberar` | Libera un lugar |

### Inscripciones

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/inscripciones?matricula=` | Inscripciones activas de un alumno |
| POST | `/inscripciones` | Inscribe. Cuerpo: `{"matricula":"...","grupo_id":N}` |
| DELETE | `/inscripciones/{matricula}/{grupo_id}` | Da de baja |

### Middleware (puerto 8500)

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/status` | Estado de todas las instancias |
| GET | `/resolve?servicio=` | Instancias sanas de un servicio |
| GET | `/routes` | Tabla de prefijos a servicios |
| POST | `/register` | Registra una instancia en tiempo de ejecución |

### Headers de diagnóstico

Cada respuesta incluye:

- `X-Instance-Id` — qué contenedor concreto atendió la petición
- `X-Backend-Service` — a qué servicio fue enrutada
- `X-Backend-Target` — la URL interna elegida por el balanceador

---

## Decisiones de diseño

### Patrón MVC

Los tres servicios separan responsabilidades de forma estricta. `models/` es el único
lugar donde se escribe SQL y no importa `net/http`. `controllers/` es el único lugar
que toca HTTP y no escribe SQL. La "vista" es la representación JSON. El cableado se
hace por inyección de dependencias en `main.go`, sin variables globales, para que un
servicio pueda instanciarse con una base distinta sin tocar su código.

### Database per service

Un contenedor de PostgreSQL con tres bases independientes: `alumnos_db`, `materias_db`
e `inscripciones_db`. Ningún servicio consulta las tablas de otro.

La consecuencia es que la tabla `inscripciones` guarda `matricula` y `grupo_id` **sin
FOREIGN KEY**: esas filas viven en otras bases y el motor no puede validarlas. La
integridad referencial se resuelve por HTTP en la cadena de validaciones. Es más lento
y más frágil que un JOIN, y ese es el costo real de los microservicios.

### Cadena de validaciones

Inscribir no es un INSERT: son seis reglas independientes evaluadas en orden, en
`models/validador.go`. Cada una es una función con la misma firma, agrupadas en un
slice que se recorre hasta la primera falla.

1. El alumno existe y está activo (HTTP a `alumnos`)
2. El grupo existe (HTTP a `materias`)
3. No está ya inscrito en ese grupo (base local)
4. No lleva otro grupo de la misma materia (base local)
5. No hay empalme de horario (HTTP concurrente a `materias`)
6. Hay cupo disponible

Agregar una regla es escribir una función y añadir una línea al slice: ninguna
validación existente se modifica. Las verificaciones locales y baratas van primero;
la validación de empalme, que dispara N llamadas HTTP, va casi al final.

### Concurrencia

El proyecto resuelve tres problemas de concurrencia distintos con tres herramientas
distintas:

**Cupo de un grupo — `SELECT ... FOR UPDATE`.** Dos peticiones simultáneas podrían leer
"29 de 30", ambas decidir que hay lugar y ambas escribir 30, dejando 31 alumnos. La
transacción bloquea la fila hasta el commit, forzando a la segunda a releer el valor
actualizado. Un `sync.Mutex` de Go no serviría: solo protege dentro de un proceso, y
hay múltiples instancias. El único árbitro compartido es la base de datos.

**Validación de empalmes — `sync.WaitGroup` + `sync.Mutex`.** Consultar el horario de
N materias ya inscritas son N llamadas HTTP independientes. Se lanzan en goroutines
paralelas: la latencia total es la de la más lenta, no la suma. Aquí sí aplica el mutex,
porque el mapa de resultados vive dentro de un solo proceso.

**Round robin — `atomic.AddUint64`.** El cursor del balanceador se incrementa sin
candados. Cada servicio tiene su propio contador en un `sync.Map`, indexado por nombre,
así que los pools de `alumnos`, `materias` e `inscripciones` rotan de forma
independiente. Es el mismo modelo de nginx: un proxy, muchos pools de upstreams.

**Worker pool — semáforo con canal.** El load balancer limita a 50 las peticiones
proxeadas simultáneamente. Sin ese límite, un pico de tráfico abriría conexiones sin
freno contra los backends y los tumbaría en lugar de solo ralentizar el proxy.

### Acción compensatoria

Apartar el lugar toca `materias_db` y el INSERT toca `inscripciones_db`. Son bases
distintas: **no existe una transacción que cubra ambas**. Si el INSERT falla después de
haber apartado el asiento, el servicio llama explícitamente a `liberar` para deshacerlo.

Esta es la versión simple del patrón Saga, y tiene un hueco conocido: si el proceso
muere justo entre el apartado y el INSERT, nadie compensa y el asiento queda fantasma.
Resolverlo requiere un log de transacciones y un proceso de reconciliación, fuera del
alcance de esta entrega.

### Tolerancia a fallos

Todo cliente HTTP saliente tiene timeout explícito (5s en los servicios, 2s en el
monitor de salud, 3s en el balanceador). Sin timeout, un servicio colgado bloquearía
goroutines indefinidamente hasta agotar al que lo llamó.

Los errores se traducen a códigos que distinguen tres situaciones que en un monolito
serían una sola:

- **404** — el recurso no existe. Respuesta negativa válida.
- **409** — existe, pero las reglas de negocio rechazan la operación.
- **503** — no se sabe, porque el servicio que tiene la respuesta no contestó.

Esa tercera categoría no existe en programación local y es la que define a los sistemas
distribuidos.

El load balancer cachea la lista de instancias sanas durante 2 segundos. Si el
middleware deja de responder, sirve la lista vencida en vez de fallar: las instancias
estaban sanas hace segundos. Se pierde la capacidad de detectar caídas nuevas, no la
de enrutar.

### Instancias sin estado

Ninguna instancia guarda datos en memoria. Todo el estado vive en PostgreSQL, y por eso
da exactamente igual cuál de las tres atienda una petición. Sin esa propiedad, el
balanceo de carga sería imposible.

---

## Pruebas

```bash
./demo.sh
```

Recorre nueve escenarios con pausas entre cada uno:

1. Estado del registro de service discovery
2. Round robin sobre las 3 instancias de `alumnos`
3. Round robin sobre `materias` con contador independiente
4. Failover: se apaga una instancia y el tráfico deja de llegarle
5. Recuperación automática al volver a levantarla
6. El sistema sigue enrutando con el middleware apagado
7. Los seis eslabones de la cadena de validaciones
8. 10 peticiones concurrentes al último lugar de un grupo lleno
9. Registro dinámico de una instancia

### Verificación manual del balanceo

```bash
for i in $(seq 1 9); do
  curl -s -D - -o /dev/null http://localhost:8000/alumnos | grep -i x-instance-id
done
```

Debe rotar entre `alumnos-1`, `alumnos-2` y `alumnos-3`.

### Verificación del failover

```bash
docker compose stop alumnos-2
sleep 7
curl -s http://localhost:8500/status | python3 -m json.tool
docker compose start alumnos-2
```

La instancia aparece como `"sano": false` y desaparece del ciclo de balanceo. Al
volver a levantarla se reincorpora en el siguiente heartbeat, sin reiniciar nada.

---

## Datos de prueba

La base se siembra con 4 alumnos, 4 materias y 6 grupos. Dos casos están preparados
deliberadamente para demostrar validaciones:

- El **grupo 02 de TC2027** está lleno (2 de 2) para probar el rechazo por cupo
- El **grupo 01 de TC1028** y el **grupo 01 de TC2027** son ambos lunes por la mañana,
  para probar el rechazo por empalme de horario

---

## Solución de problemas

| Síntoma | Causa y solución |
|---|---|
| `permission denied` al correr docker | Falta el grupo: `sudo usermod -aG docker $USER` y luego `newgrp docker` |
| Un contenedor en `Restarting` | `docker compose logs <nombre>` |
| `"sanas": 0` en `/status` | Espera 10s al primer ciclo de heartbeat |
| Cambios en los `.sql` no se aplican | Los scripts de init solo corren con el volumen vacío: `docker compose down -v` |
| El frontend carga pero sin datos | Revisa CORS en la consola del navegador y recarga con Ctrl+Shift+R |
| Puerto ocupado al levantar | `docker compose down` y verifica con `ss -ltn` |

---

## Stack

| Capa | Tecnología |
|---|---|
| Backend | Go 1.22, biblioteca estándar. Única dependencia: `github.com/lib/pq` |
| Base de datos | PostgreSQL 16 (Alpine) |
| Frontend | React 18 + Vite, servido por Nginx |
| Contenedores | Docker + Docker Compose v2 |

Las imágenes de los servicios usan build multi-etapa con `CGO_ENABLED=0`, lo que produce
binarios estáticos y imágenes finales de aproximadamente 15 MB.
