# Sistema Distribuido de Inscripción de Materias

Proyecto de Cómputo Distribuido. Sistema de inscripción universitaria construido como
microservicios en Go. nginx funciona como reverse proxy y load balancer, Consul lleva el
registro de servicios con health checks, y Kafka lleva los eventos entre servicios de
forma asíncrona. En paralelo corre un load balancer escrito a mano en Go, para comparar.

**19 contenedores:** 1 frontend, 1 nginx gateway, 1 load balancer de Go, 1 middleware,
1 Consul, 1 Kafka, 1 kafka-init (crea los tópicos y termina), 11 instancias de servicio
(3 de alumnos, 3 de materias, 3 de inscripciones y 2 de notificaciones) y 1 base de datos.

---

## Entregas

Cada entrega se acumula sobre la anterior; nada se reemplazó.

| Entrega | Qué se agregó |
|---|---|
| Base | Tres servicios MVC con PostgreSQL, middleware de service discovery, load balancer en Go, frontend, `setup.sh` |
| nginx | nginx como reverse proxy y load balancer, en su propio contenedor, entre el frontend y los servicios |
| Consul | Registro y borrado automáticos en Consul, health checks, Consul en su propio contenedor |
| Kafka | Cola de mensajería asíncrona: 4 tópicos, una cola (consumer group) por servicio que consume, servicio nuevo de notificaciones |
| Mejoras de uso | Lista de espera (por Kafka), avisos visibles para el alumno, reglas nuevas (créditos, prerrequisitos, periodo), historial, frontend nuevo, 200 alumnos de prueba, `simulacro.sh` y `verificar_consistencia.sh` |

---

## Arquitectura

```
                      ┌──────────────┐
                      │   Frontend   │  React + Vite  :3000
                      └──────┬───────┘
                             │
                      ┌──────▼────────────┐
                      │  nginx gateway    │  :8000  ← puerta principal
                      │  reverse proxy    │
                      │  + load balancer  │
                      └──────┬────────────┘
                             │
            ┌────────────────┼────────────────┐
            │                │                │
    ┌───────▼──────┐ ┌───────▼──────┐ ┌───────▼──────┐
    │   alumnos    │ │   materias   │ │inscripciones │
    │   x3         │ │   x3         │ │   x3         │
    └───────┬──────┘ └───────┬──────┘ └───────┬──────┘
            │                │                │
            └────────────────┼────────────────┘
                      ┌──────▼───────┐
                      │  PostgreSQL  │  3 bases separadas  :5432
                      └──────────────┘

   Mensajería asíncrona (sección "Mensajería asíncrona con Kafka"):

     alumnos ───────┐                                   ┌──> materias x3 (devuelve lugares)
     inscripciones ─┼──> Kafka  5 tópicos, 3 particiones┼──> inscripciones x3 (lista de espera)
     materias ──────┘    kafka:9092, solo en la red     └──> notificaciones x2 (avisos)

   Service discovery (sección "Service discovery con Consul"):

     cada instancia ── registro y borrado ──> Consul :8500 ── health check cada 5s ──> /heartbeat

   Ruta paralela, para comparar las dos implementaciones del gateway:

                      ┌───────────────────┐        ┌──────────────────┐
   cliente ────────>  │ load balancer Go  │ ·····> │   Middleware     │  :8600
                      │ :8001             │ <····· │ service discovery│
                      └─────────┬─────────┘        │ health monitor   │
                                │                  └────────┬─────────┘
                                └──> mismo pool de           │ heartbeat
                                     11 instancias <─────────┘  cada 5s
```

Las líneas continuas son tráfico real. Las punteadas son metadatos.

nginx es la puerta principal: por ahí entra el navegador y por ahí pasan también las
llamadas HTTP entre servicios. El load balancer de Go queda expuesto en el 8001 para poder
contrastar las dos aproximaciones en vivo.

Hay dos formas de comunicación entre servicios, y cada una se usa donde le toca:

- **HTTP, síncrona**, cuando el que llama necesita la respuesta para seguir. Para inscribir,
  `inscripciones` tiene que saber *ya* si el alumno existe y si hay cupo.
- **Kafka, asíncrona**, para avisar que algo ya pasó. Cuando un alumno se da de baja,
  `inscripciones` no necesita esperar a que `materias` devuelva el lugar: publica el hecho
  y responde.

---

## Las dos implementaciones del gateway

El mismo problema —repartir peticiones entre instancias sanas— resuelto de dos formas.

| | nginx (`:8000`) | Go (`:8001`) |
|---|---|---|
| Cómo se expresa | 30 líneas de configuración declarativa | ~200 líneas de código |
| Algoritmo | round robin (default de nginx) | round robin con `atomic.AddUint64` |
| Pools | un `upstream` por servicio | un contador por servicio en un `sync.Map` |
| Health check | **pasivo**: `max_fails` / `fail_timeout` | **activo**: el middleware sondea `/heartbeat` cada 5s |
| Service discovery | estático, en el `nginx.conf` | dinámico, vía `/resolve` del middleware |
| Reintento | `proxy_next_upstream` | lista rotada, se intenta la siguiente instancia |
| Madurez | probado en producción desde 2004 | escrito para esta materia |

La diferencia más visible es el health checking. nginx open source **no sondea** los
backends: observa peticiones reales y, tras `max_fails=3` fallos dentro de
`fail_timeout=30s`, saca esa instancia de rotación. Los chequeos activos son función de
nginx Plus.

La consecuencia es medible y `pruebas.sh` la demuestra: al apagar una instancia, por
nginx se cuelan uno o dos `502` antes de que la saque; por el load balancer de Go no
falla ninguna, porque el middleware ya la había marcado como caída.

A cambio, nginx no necesita ningún componente extra, se configura sin compilar y lleva
dos décadas en producción. El código de Go da descubrimiento dinámico y detección
proactiva, al costo de ser código propio que hay que mantener.

**El paso siguiente natural** sería `consul-template`, que regenera el `nginx.conf` y
recarga nginx cuando el registro de Consul cambia. Eso junta las dos ventajas: el proxy
maduro y el descubrimiento dinámico.

---

## Requisitos

- Linux, familia Debian (Debian, Ubuntu, Kali) o RHEL (Rocky, Alma, Fedora, RHEL)
- Docker Engine 24+ con el plugin Docker Compose v2
- Unos 2 GB de RAM libres (Kafka reserva hasta 512 MB para su JVM)
- Puertos libres: 3000, 5432, 8000, 8001, 8081-8083, 8091-8093, 8101-8103, 8500, 8600

El script de instalación detecta la distribución e instala Docker si falta.

---

## Instalación y ejecución

```bash
git clone https://github.com/USUARIO/REPOSITORIO.git
cd REPOSITORIO
chmod +x setup.sh pruebas.sh pruebas_kafka.sh simulacro.sh verificar_consistencia.sh
./setup.sh
```

El script detecta la familia del sistema, instala Docker, genera el `.env`, construye
las imágenes, levanta los contenedores, revisa que Kafka tenga sus tópicos y espera a que
las 11 instancias que vigila el middleware reporten sanas. La primera ejecución tarda varios
minutos compilando.

Para reconstruir desde cero borrando los datos:

```bash
./setup.sh --limpio
```

**Si vienes de una versión anterior del proyecto**, usa `--limpio` la primera vez: las
mejoras agregan tablas, una base nueva (`notificaciones_db`) y los datos de prueba, y los
scripts de `db/init` solo corren con el volumen de Postgres vacío.

Una vez arriba:

| Recurso | URL |
|---|---|
| Frontend | http://localhost:3000 |
| nginx gateway | http://localhost:8000 |
| Load balancer de Go | http://localhost:8001 |
| Consul (interfaz web) | http://localhost:8500/ui |
| Estado del middleware | http://localhost:8600/status |
| Kafka | sin puerto en el host: `docker compose exec kafka ...` (ver sección de Kafka) |

Para apagar todo: `docker compose down`

### Cómo se usa

En http://localhost:3000 se elige un alumno y la pantalla muestra todo lo suyo:

- **Horario** de la semana con sus materias, y el total de créditos.
- **Catálogo**, con un buscador. Cada grupo dice si el alumno puede inscribirse y, si no,
  por qué: *Inscrito*, *Empalma con TC1028*, *Requiere TC1028*, *Ya aprobada*, *Ya llevas
  esta materia*. Si el grupo está lleno, el botón cambia a **Formarme** (lista de espera).
- **Mis materias**, con un botón de baja que pide confirmación.
- **Lista de espera**: en qué filas está formado y en qué lugar va, o por qué salió.
- **Avisos**: los mensajes que le mandó el servicio de notificaciones. Si se libera un
  lugar y el alumno era el primero de la fila, aquí aparece solo que ya quedó inscrito.
- **Historial** (desplegable): todo lo que inscribió y dio de baja, con fechas.
- **Coordinación** (desplegable): avisos de grupos que se llenaron.
- **Bitácora de instancias**: qué contenedor contestó cada petición; ahí se ve el round
  robin. Las consultas automáticas (cada 4 segundos) no se anotan, para no llenarla.

Para probar las reglas con los alumnos originales:

| Alumno | Qué demuestra |
|---|---|
| Ana (`A01234567`) | Inscribe TC1028-01 y luego mira TC2027-01: *Empalma con TC1028* (los dos son lunes en la mañana) |
| Bruno (`A01234568`) | Ya aprobó TC1028, así que puede tomar TC2005 Sistemas Operativos; TC1028 sale como *Ya aprobada* |
| Diego (`A01234570`) | Primer semestre: TC2005 sale como *Requiere TC1028* |
| Cualquiera | TC2027-02 está lleno (2/2): **Formarme**, y en otra pestaña dar de baja a uno de sus inscritos |

### Notas por sistema

**RHEL / Rocky / Fedora.** Si tienes el paquete `podman-docker`, el comando `docker` es
en realidad un alias de Podman, y no construye las imágenes locales de este proyecto.
`setup.sh` lo detecta y lo reemplaza por Docker real; Podman en sí se conserva.

**SELinux.** Los bind mounts del `docker-compose.yml` llevan la opción `:z` para que
Docker reetiquete los archivos. Sin ella, con SELinux en enforcing, Postgres no podría
leer `db/init` ni `kafka-init` su script de tópicos. La opción es de Docker, así que en
sistemas sin SELinux se ignora: el archivo sigue siendo portable.

---

## Estructura del proyecto

```
.
├── db/
│   ├── generar_datos.py      Genera los datos de prueba (10-, 11- y 12-)
│   └── init/                 Scripts SQL de inicialización, en orden
│       ├── 01-crear-bases.sql
│       ├── 02-alumnos.sql        tablas
│       ├── 03-materias.sql
│       ├── 04-inscripciones.sql
│       ├── 05-notificaciones.sql
│       ├── 10-datos-alumnos.sql  datos generados
│       ├── 11-datos-materias.sql
│       └── 12-datos-inscripciones.sql
├── kafka/
│   └── crear-topicos.sh      Declara los 5 tópicos (lo ejecuta kafka-init)
├── services/
│   ├── alumnos/              Servicio de estudiantes y materias aprobadas
│   ├── materias/             Catálogo, grupos, cupos y prerrequisitos
│   ├── inscripciones/        Orquestación, inscripción y lista de espera
│   └── notificaciones/       Guarda y sirve los avisos que llegan por Kafka
├── nginx-gateway/            Reverse proxy + load balancer declarativo
│   ├── nginx.conf
│   └── Dockerfile
├── loadbalancer/             Reverse proxy + round robin en Go
│   └── balanceador.go
├── middleware/               Service discovery + health monitor
│   ├── registro.go
│   ├── monitor.go
│   └── routes.json
├── frontend/                 React + Vite
├── docker-compose.yml
├── setup.sh                  Instalación y arranque automatizados
├── pruebas.sh                Verificación escenario por escenario
├── pruebas_kafka.sh          Verificación de la mensajería con Kafka
├── simulacro.sh              "Día de inscripciones": 40 alumnos por 10 lugares
└── verificar_consistencia.sh ¿Cuadran los datos entre las bases?
```

Cada servicio sigue la misma estructura MVC:

```
servicio/
├── models/          Structs y acceso a datos. Único lugar con SQL.
├── controllers/     Entradas: handlers HTTP y, en materias y notificaciones, handlers de eventos
├── connectors/      Salidas: HTTP a otros servicios y productor de Kafka
├── events/          Contrato de los eventos: tópicos, grupos y structs (igual en todos)
├── kafkax/          Envoltura de kafka-go: productor y consumidor (igual en todos)
├── consul.go        Registro, health check y borrado en Consul
├── main.go          Inyección de dependencias y ruteo
└── Dockerfile       Build multi-etapa: golang-alpine → alpine
```

---

## El gateway de nginx

`nginx-gateway/nginx.conf` declara un `upstream` por servicio:

```nginx
upstream alumnos_pool {
    server alumnos-1:8080 max_fails=3 fail_timeout=30s;
    server alumnos-2:8080 max_fails=3 fail_timeout=30s;
    server alumnos-3:8080 max_fails=3 fail_timeout=30s;
}
```

Sin directiva de balanceo, nginx usa round robin, que es la misma política que el
código de Go implementa a mano. Las alternativas (`least_conn`, `ip_hash`, pesos) están
comentadas en el archivo para poder compararlas vía el header `X-Instance-Id`.

Los pools son independientes entre sí: tráfico hacia `alumnos` no mueve el cursor de
`materias`. Esto es exactamente lo que hace el `sync.Map` de contadores del balanceador
de Go, y es la razón por la que un solo proxy basta para los tres servicios —el mismo
modelo que usa nginx en producción, con varios bloques `upstream` en un solo proceso.

`/materias` y `/grupos` apuntan al mismo pool, porque ambos prefijos pertenecen al
servicio de materias. Es el equivalente de los dos prefijos listados bajo `materias` en
el `routes.json` del middleware.

**CORS vive aquí.** Los headers están a nivel `server` para que todas las locations los
hereden, y las peticiones `OPTIONS` de preflight se responden con `204` sin llegar a
ningún backend. La línea crítica es `Access-Control-Expose-Headers`: sin ella el
navegador recibe el `X-Instance-Id` pero no deja que el JavaScript lo lea, y la bitácora
de instancias del frontend se queda vacía.

**nginx resuelve los nombres de los upstream al arrancar.** Si una instancia no existe,
falla con `host not found in upstream`. Por eso su bloque en el compose lleva
`depends_on` con las nueve. Por la misma razón, si se recrean los backends hay que
reiniciar nginx: `docker compose restart nginx-gateway`.

---

## Service discovery con Consul

Consul corre en su propio contenedor, en modo dev (un nodo, en memoria), con su interfaz
web en http://localhost:8500/ui. El middleware propio se movió al puerto 8600 del host
para dejarle el 8500, que es el puerto estándar de Consul.

**Registro.** Cada instancia se registra sola al arrancar con un `PUT` a
`/v1/agent/service/register`, usando solo `net/http`, sin SDK (`consul.go` de cada
servicio). Se registra con su nombre de contenedor como ID y dirección, y con el nombre
lógico del servicio: `alumnos-1`, `alumnos-2` y `alumnos-3` responden todos a `alumnos`.
Reintenta hasta 10 veces, porque `depends_on` solo garantiza que el contenedor de Consul
arrancó, no que su API ya acepte peticiones.

**Health check.** El registro le dice a Consul cómo revisar la salud de la instancia, y
desde ahí Consul se encarga de sondearla:

```json
"Check": {
  "HTTP": "http://alumnos-1:8080/heartbeat",
  "Interval": "5s",
  "Timeout": "2s",
  "DeregisterCriticalServiceAfter": "1m"
}
```

**Borrado.** Al recibir `SIGTERM` (lo que manda `docker stop`) la instancia llama a
`/v1/agent/service/deregister/<id>` y después apaga el servidor HTTP. El borrado es
inmediato, sin esperar a que falle el check. Si la instancia muere sin despedirse
(`docker kill` manda `SIGKILL`, que no se puede capturar), su check pasa a `critical` y
Consul la elimina sola al minuto por `DeregisterCriticalServiceAfter`.

El servicio de notificaciones también se registra: Consul vigila su `/heartbeat` igual
que el de los demás.

```bash
# Instancias sanas de un servicio
curl -s "http://localhost:8500/v1/health/service/alumnos?passing=true" \
  | python3 -c "import sys,json; print(len(json.load(sys.stdin)),'sanas')"

# Registro y borrado en vivo
docker compose stop alumnos-2         # -> 2 sanas, inmediato
docker compose start alumnos-2        # -> 3 sanas, en unos segundos
```

---

## Mensajería asíncrona con Kafka

### Por qué una cola de mensajes

Antes, al dar de baja una inscripción, `inscripciones` llamaba por HTTP a
`materias` para devolver el lugar. Si `materias` estaba caído, la baja quedaba registrada
pero el lugar nunca se devolvía: solo quedaba un `ADVERTENCIA` en el log.

Ahora `inscripciones` publica el hecho "esta inscripción se canceló" en Kafka y responde.
`materias`, que es el dueño de los cupos, lee el evento y devuelve el lugar. Si `materias`
está caído, el evento espera en Kafka y se procesa cuando regresa. Y el mismo evento le
llega también a `notificaciones`, sin que `inscripciones` sepa que ese servicio existe.

El precio es la **consistencia eventual**: durante un instante (milisegundos, o lo que
tarde `materias` en volver) la baja ya está registrada y el lugar todavía se ve ocupado.

### Tópicos y colas

Un **tópico** es una lista de eventos de un mismo tipo. Cada servicio publica en sus
propios tópicos los hechos que le pasan a él:

| Tópico | Lo publica | Cuándo | Clave | Lo consumen (grupos) |
|---|---|---|---|---|
| `alumnos.registrados` | alumnos | `POST /alumnos` | matrícula | notificaciones |
| `inscripciones.creadas` | inscripciones | `POST /inscripciones` | matrícula | notificaciones |
| `inscripciones.canceladas` | inscripciones | `DELETE /inscripciones/...` | matrícula | **materias**, notificaciones |
| `grupos.llenos` | materias | se ocupa el último lugar de un grupo | id del grupo | notificaciones |
| `lugares.liberados` | materias | **después** de devolver un lugar por una baja | id del grupo | **inscripciones** (lista de espera) |

Una **cola** es un *consumer group*. Todas las instancias de un servicio entran a Kafka
con el mismo nombre de grupo, y eso tiene dos efectos:

- **Dentro de un grupo, cada evento lo procesa una sola instancia.** Kafka reparte las
  particiones del tópico entre las instancias del grupo: con 3 particiones y 3 instancias
  de `materias`, a cada una le toca una. Así se balancea el trabajo y ninguna baja
  devuelve el lugar dos veces.
- **Grupos distintos reciben cada uno su propia copia.** El grupo `materias` y el grupo
  `notificaciones` leen el mismo tópico `inscripciones.canceladas` sin estorbarse: cada
  grupo lleva su propia posición (offset) en el tópico.

```
                           inscripciones.canceladas
                    ┌──────────────┬──────────────┬──────────────┐
                    │ partición 0  │ partición 1  │ partición 2  │
                    └──────┬───────┴──────┬───────┴──────┬───────┘
                           │              │              │
   grupo materias          ├──> materias-1├──> materias-2├──> materias-3
                           │              │              │
   grupo notificaciones    └──> notif-1   └──> notif-2   └──> notif-2
```

Cada partición le entrega sus eventos a **una** instancia de **cada** grupo. Con 3
particiones y 2 instancias de notificaciones, a una le tocan dos.

Las 3 particiones fijan el paralelismo máximo: hasta 3 instancias de un grupo trabajando
a la vez. Una cuarta instancia se quedaría sin partición, esperando de reserva.

**La clave decide la partición.** El productor calcula `hash(clave)`, y todos los eventos
con la misma clave caen en la misma partición. Dentro de una partición Kafka respeta el
orden, así que los eventos de un mismo alumno se leen en el orden en que pasaron.

### Formato de los eventos

Son JSON. Los structs están en `events/events.go`, el mismo archivo en todos los
servicios: es el contrato entre quien publica y quien consume.

```json
{
  "evento_id": "0947450e-d6e4-4855-b190-e8d472e930e2",
  "inscripcion_id": 7,
  "matricula": "A01234567",
  "grupo_id": 6,
  "materia_clave": "TC3045",
  "fecha": "2026-10-23T17:24:02Z"
}
```

Un evento es un hecho que ya ocurrió, por eso los tópicos se nombran en pasado. No es una
orden: `inscripciones` no le dice a `materias` "libera el lugar"; avisa que hubo una baja,
y cada quien decide qué hacer con eso.

### Entrega "al menos una vez" e idempotencia

El consumidor confirma el offset (commit) **después** de procesar el evento, nunca antes.
Si una instancia muere a la mitad, el evento no se pierde: se le entrega a otra instancia o
a la misma cuando regresa. La otra cara es que **un evento puede llegar dos veces**, así
que procesarlo dos veces no debe cambiar el resultado. Para eso sirve `evento_id`:

- **materias** guarda cada `evento_id` procesado en la tabla `eventos_procesados`, en la
  misma transacción en la que devuelve el lugar. Si el evento ya estaba, el `INSERT ... ON
  CONFLICT DO NOTHING` no inserta nada y el cupo no se toca. Como está en la base, funciona
  aunque el duplicado lo reciba otra de las tres instancias.
- **notificaciones** guarda cada aviso en su propia base (`notificaciones_db`) con el
  `evento_id` como `UNIQUE`. Un evento repetido no inserta nada, sin importar cuál de sus
  dos instancias lo reciba. (En la entrega de Kafka lo recordaba en memoria, como el
  `inventory-service` de `kafka_example`; al guardar los avisos para mostrarlos, la
  memoria pasó a la base y se fue esa limitación.)
- **inscripciones**, al promover la lista de espera, no necesita tabla: si
  `lugares.liberados` llega dos veces, el segundo intento se encuentra con que el lugar ya
  se ocupó (`materias` lo cuida con `FOR UPDATE`) y no inscribe a nadie de más.

Si un handler falla, se reintenta hasta 3 veces, esperando un poco más cada vez. Un mensaje que no es JSON
válido no se reintenta: nunca va a funcionar, así que se registra en el log y se sigue.

### El código

La integración sigue la estructura de `kafka_example` de la clase, acomodada al MVC del
proyecto:

| Archivo | Qué hace |
|---|---|
| `kafkax/kafkax.go` | `NewWriter`, `Publish` y `Consume`: la envoltura de `segmentio/kafka-go` |
| `events/events.go` | Nombres de tópicos y grupos, y los structs de los eventos |
| `connectors/eventos_connector.go` | Productor del servicio: un método por evento que publica |
| `controllers/eventos_controller.go` | (materias) Handler de `inscripciones.canceladas`; (inscripciones) handler de `lugares.liberados` |
| `controllers/notificacion_controller.go` | (notificaciones) Handler de los 4 tópicos de avisos y la API `/notificaciones` |
| `models/eventos_model.go` | (materias) `LiberarPorEvento` y la tabla `eventos_procesados` |
| `models/lista_espera_model.go` | (inscripciones) La fila: formarse, posición, salir, el primero |

El productor es un **connector** porque es una salida del servicio hacia otro sistema,
igual que el cliente HTTP de `inscripciones`. El consumidor es un **controller** porque es
una entrada: igual que un handler HTTP recibe una petición y llama al modelo, el handler de
Kafka recibe un evento y llama al mismo modelo.

En `main.go`, el consumidor corre en una goroutine junto al servidor HTTP. Al recibir
`SIGTERM`, el servicio se desregistra de Consul, apaga el HTTP y después cierra el
consumidor, que sale de su grupo para que Kafka le pase sus particiones a otra instancia
al momento.

### La infraestructura

- **Kafka 3.8 en modo KRaft**, un solo nodo que hace de broker y de controlador, sin
  ZooKeeper. Es la configuración de `kafka_example` de la clase, salvo el listener
  anunciado: aquí los clientes son otros contenedores, así que Kafka se anuncia como
  `kafka:9092` en lugar de `localhost:9092`.
- **`kafka-init`** es un contenedor de un solo uso: espera a que Kafka esté sano, corre
  `kafka/crear-topicos.sh` y termina. Que aparezca como `exited (0)` es lo normal. Los
  servicios no arrancan hasta que termina bien (`service_completed_successfully`).
- **La creación automática de tópicos está apagada**
  (`KAFKA_AUTO_CREATE_TOPICS_ENABLE=false`). Sin eso, un consumidor que preguntara por un
  tópico mal escrito lo crearía solo, con una partición, y el error pasaría desapercibido.

### Probarlo

```bash
./pruebas_kafka.sh
```

Primero revisa que nginx mande cada ruta a su propio servicio (si no, lo reinicia; ver Solución de problemas). Después recorre nueve pruebas y reporta `[PASA]` o `[FALLA]` en cada comprobación:

1. Los 5 tópicos existen con 3 particiones
2. Los tres consumer groups tienen todas sus particiones repartidas entre sus instancias
3. Registrar un alumno llega a `notificaciones`
4. Inscribir llega a `notificaciones`
5. **Una baja la procesa una sola instancia de `materias`, y `notificaciones` recibe su
   propia copia** (cola + fan-out)
6. **Idempotencia**: se vuelve a publicar el mismo evento de baja; ni el lugar se devuelve
   dos veces ni el aviso se manda dos veces
7. Llenar un grupo publica `grupos.llenos`
8. **Con las 3 instancias de `materias` apagadas, la baja responde 200**; el evento espera
   en la cola (se ve el `LAG`) y se procesa cuando `materias` regresa
9. **Lista de espera**: un alumno se forma en un grupo lleno, alguien se da de baja y el
   alumno queda inscrito solo, con su aviso. Al final el grupo queda como estaba

Lo mismo a mano. Las herramientas de Kafka vienen dentro de su contenedor:

```bash
K="docker compose exec kafka /opt/kafka/bin"

# Tópicos y particiones
$K/kafka-topics.sh --bootstrap-server localhost:9092 --describe

# Qué instancia tiene cada partición (columna CLIENT-ID) y cuántos eventos faltan (LAG)
$K/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group materias
$K/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group inscripciones
$K/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group notificaciones

# Ver los eventos de un tópico desde el principio (Ctrl+C para salir)
$K/kafka-console-consumer.sh --bootstrap-server localhost:9092 \
  --topic inscripciones.canceladas --from-beginning --property print.key=true

# Seguir los logs de los consumidores mientras se usa el frontend
docker compose logs -f materias-1 materias-2 materias-3 notificaciones-1 notificaciones-2
```

Para ver el reparto entre instancias: apaga una (`docker compose stop notificaciones-2`),
vuelve a describir el grupo y verás que la otra se quedó con todas sus particiones;
préndela otra vez y se vuelven a repartir.

---

## Lista de espera

Cuando un grupo está lleno, el alumno se puede formar (`POST /lista-espera`). Cuando se
libera un lugar, el primero de la fila queda inscrito solo, sin volver a pedirlo:

```
 alumno da de baja ──> inscripciones.canceladas ──> materias devuelve el lugar
                                                         │
                                                         ▼
 aviso al alumno <── inscripciones.creadas <── inscripciones inscribe al  <── lugares.liberados
 ("se liberó un lugar     (desde_lista_espera)    primero de la fila
  y quedaste inscrito")
```

Las decisiones que la hacen funcionar:

- **`materias` publica `lugares.liberados` después de devolver el lugar, no antes.** Si
  `inscripciones` reaccionara directo a `inscripciones.canceladas`, podría intentar apartar
  el lugar antes de que `materias` lo devolviera, y encontraría el grupo todavía lleno.
- **Nadie se adelanta a la fila.** Hay una regla nueva en la cadena (`validarSinFila`): si
  hay alumnos esperando un grupo, inscribirse directo se rechaza con 409, salvo para el
  primero de la fila. Sin ella, un lugar recién liberado se lo llevaría quien hiciera clic
  más rápido.
- **El orden es el de llegada.** La fila es una tabla en `inscripciones_db` (`lista_espera`)
  y el orden es el `id`, que Postgres asigna en el orden en que se insertan las filas. Un
  índice único impide formarse dos veces en el mismo grupo, aun si las dos peticiones caen
  en instancias distintas.
- **Al promover se pasa por la cadena completa.** Si mientras esperaba el alumno se
  inscribió en otro grupo que ahora le empalma, o se pasaría de créditos, no entra: sale de
  la fila con el motivo (`CANCELADA`, se ve en su pantalla) y el lugar pasa al siguiente.
- **Una sola instancia atiende cada grupo.** `lugares.liberados` usa el id del grupo como
  clave, así que todas las liberaciones de un grupo caen en la misma partición y las
  procesa una instancia, una por una. Dos instancias nunca promueven la fila del mismo
  grupo al mismo tiempo.

Para verlo con muchos alumnos a la vez: `./simulacro.sh` (sección [Pruebas](#pruebas)).

---

## API

Todas las rutas se consumen a través de nginx en el puerto 8000. El load balancer de Go
sirve exactamente las mismas en el 8001.

### Alumnos

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/alumnos` | Lista de alumnos activos |
| GET | `/alumnos/{matricula}` | Un alumno, con las materias que ya aprobó (`aprobadas`) |
| POST | `/alumnos` | Registra un alumno. Publica `alumnos.registrados` |

### Materias

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/materias` | Catálogo con grupos anidados, créditos y prerrequisitos (`requiere`) |
| GET | `/materias/{clave}` | Una materia con sus grupos |
| GET | `/grupos/{id}` | Un grupo |
| POST | `/grupos/{id}/apartar` | Aparta un lugar. Si era el último, publica `grupos.llenos` |
| POST | `/grupos/{id}/liberar` | Libera un lugar |

### Inscripciones

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/inscripciones?matricula=` | Inscripciones activas de un alumno |
| GET | `/inscripciones?matricula=&historial=1` | Todas, también las dadas de baja (con `baja_en`) |
| POST | `/inscripciones` | Inscribe. Cuerpo: `{"matricula":"...","grupo_id":N}`. Publica `inscripciones.creadas` |
| DELETE | `/inscripciones/{matricula}/{grupo_id}` | Da de baja. Publica `inscripciones.canceladas`; el lugar lo devuelve `materias` al leer el evento |
| POST | `/lista-espera` | Se forma en un grupo lleno. Cuerpo: `{"matricula":"...","grupo_id":N}`. Responde su posición |
| GET | `/lista-espera?matricula=` | Dónde está formado un alumno (y en qué terminaron sus filas anteriores) |
| GET | `/lista-espera?grupo_id=` | La fila actual de un grupo, en orden |
| DELETE | `/lista-espera/{matricula}/{grupo_id}` | Se sale de la fila |

### Notificaciones

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/notificaciones?matricula=` | Los avisos de un alumno, el más nuevo primero |
| GET | `/notificaciones/coordinacion` | Avisos para coordinación (grupos que se llenaron) |

### Middleware (puerto 8600)

| Método | Ruta | Descripción |
|---|---|---|
| GET | `/status` | Estado de todas las instancias |
| GET | `/resolve?servicio=` | Instancias sanas de un servicio |
| GET | `/routes` | Tabla de prefijos a servicios |
| POST | `/register` | Registra una instancia en tiempo de ejecución |

### Headers de diagnóstico

- `X-Instance-Id` — qué contenedor atendió la petición (lo pone el servicio)
- `X-Backend-Service` / `X-Backend-Target` — a dónde enrutó (solo el balanceador de Go)

Al inspeccionar con `curl -D -`, ancla el grep al inicio de línea
(`grep -i "^x-instance-id"`): sin el `^` también atrapa la línea
`Access-Control-Expose-Headers`, que menciona ese nombre como valor.

---

## Decisiones de diseño

### Patrón MVC

Los servicios separan responsabilidades de forma estricta. `models/` es el único lugar
donde se escribe SQL y no importa `net/http`. `controllers/` son las entradas: los
handlers HTTP y los handlers de eventos de Kafka; no escriben SQL. `connectors/` son las
salidas: el cliente HTTP hacia otros servicios y el productor de Kafka. La "vista" es la
representación JSON. El cableado se hace por inyección de dependencias en `main.go`, sin
variables globales.

### Database per service

Un contenedor de PostgreSQL con cuatro bases independientes: `alumnos_db`, `materias_db`,
`inscripciones_db` y `notificaciones_db`. Ningún servicio consulta las tablas de otro.

La consecuencia es que la tabla `inscripciones` guarda `matricula` y `grupo_id` **sin
FOREIGN KEY**: esas filas viven en otras bases y el motor no puede validarlas. La
integridad referencial se resuelve por HTTP en la cadena de validaciones. Es más lento y
más frágil que un JOIN, y ese es el costo real de los microservicios.

### Cadena de validaciones

Inscribir no es un INSERT: son once reglas independientes evaluadas en orden, en
`models/validador.go`. Cada una es una función con la misma firma, agrupadas en un slice
que se recorre hasta la primera falla.

1. **El periodo de inscripción está abierto** (solo compara la hora)
2. El alumno existe y está activo (HTTP a `alumnos`, vía nginx)
3. El grupo existe (HTTP a `materias`, vía nginx)
4. **No aprobó ya esa materia** (las aprobadas vienen de `alumnos`)
5. **Aprobó los prerrequisitos** (lo que pide la materia viene de `materias`; lo que aprobó, de `alumnos`)
6. No está ya inscrito en ese grupo (base local)
7. No lleva otro grupo de la misma materia (base local)
8. No hay empalme de horario (HTTP concurrente a `materias`, vía nginx)
9. **No se pasa del límite de créditos** (reutiliza los grupos que trajo la regla 8)
10. **Nadie lo espera en la lista de espera**, salvo que él sea el primero
11. Hay cupo disponible

Las reglas en negritas llegaron con las mejoras, y entraron justo como promete el diseño:
una función y una línea en el slice cada una, sin tocar las demás. La de prerrequisitos es
un buen ejemplo de por qué la cadena vive en `inscripciones`: necesita datos de dos
servicios a la vez, y ninguno de los dos podría validarla solo.

Formarse en la lista de espera usa casi la misma cadena (`cadenaFila`): si el alumno no
podría inscribirse, tampoco tiene sentido que espere un lugar. Cambian las dos últimas
reglas: para formarse, el grupo **no** debe tener lugar.

El límite de créditos y el periodo se configuran en `docker-compose.yml`, en el bloque
`x-reglas-inscripcion` (`MAX_CREDITOS`, `INSCRIPCION_INICIO`, `INSCRIPCION_FIN`). Para
probar el rechazo por periodo, pon una fecha ya pasada en `INSCRIPCION_FIN` y recrea
inscripciones: `docker compose up -d inscripciones-1 inscripciones-2 inscripciones-3`.

Como `inscripciones` llama a los otros servicios a través de nginx, **una sola petición de
usuario genera cuatro o más entradas en el log del gateway**. Se ve corriendo
`docker compose logs -f nginx-gateway` mientras se lanza una inscripción: las líneas con
user-agent `Go-http-client` vienen de los servicios, no del cliente.

### Concurrencia

Tres problemas distintos resueltos con tres herramientas distintas:

**Cupo de un grupo — `SELECT ... FOR UPDATE`.** Dos peticiones simultáneas podrían leer
"29 de 30", ambas decidir que hay lugar y ambas escribir 30, dejando 31 alumnos. La
transacción bloquea la fila hasta el commit. Un `sync.Mutex` de Go no serviría: solo
protege dentro de un proceso, y hay múltiples instancias. El único árbitro compartido es
la base de datos.

**Validación de empalmes — `sync.WaitGroup` + `sync.Mutex`.** Consultar el horario de N
materias ya inscritas son N llamadas HTTP independientes. Se lanzan en goroutines
paralelas: la latencia total es la de la más lenta, no la suma.

**Round robin — `atomic.AddUint64`.** El cursor del balanceador de Go se incrementa sin
candados. Cada servicio tiene su propio contador en un `sync.Map`.

**Worker pool — semáforo con canal.** El balanceador de Go limita a 50 las peticiones
proxeadas simultáneamente, para que un pico de tráfico ralentice el proxy en lugar de
tumbar los backends.

**Eventos repetidos — `PRIMARY KEY` en `eventos_procesados`.** Si dos instancias de
`materias` reciben el mismo evento a la vez (puede pasar durante un rebalanceo), la
segunda espera a que la primera termine su transacción, encuentra el `evento_id` ya
guardado y no toca el cupo. Otra vez, el árbitro es la base de datos.

### Acción compensatoria

Apartar el lugar toca `materias_db` y el INSERT toca `inscripciones_db`. Son bases
distintas: **no existe una transacción que cubra ambas**. Si el INSERT falla después de
haber apartado el asiento, el servicio llama explícitamente a `liberar` para deshacerlo.

Es la versión simple del patrón Saga, con un hueco conocido: si el proceso muere entre el
apartado y el INSERT, nadie compensa y el asiento queda fantasma. Resolverlo requiere un
log de transacciones y reconciliación, fuera del alcance de esta entrega.

La baja ya no usa esta llamada directa: se comunica por el evento
`inscripciones.canceladas`, que sobrevive aunque `materias` esté caído.

### Tolerancia a fallos

Todo cliente HTTP saliente tiene timeout explícito (5s en los servicios, 2s en el monitor
de salud, 3s en el balanceador). Publicar en Kafka también: si el broker no contesta en 5s,
el servicio registra la advertencia en lugar de quedarse colgado. Sin timeout, un servicio
colgado bloquearía goroutines hasta agotar al que lo llamó.

Los errores se traducen a códigos que distinguen tres situaciones que en un monolito
serían una sola:

- **404** — el recurso no existe. Respuesta negativa válida.
- **409** — existe, pero las reglas de negocio rechazan la operación.
- **503** — no se sabe, porque el servicio que tiene la respuesta no contestó.

Esa tercera categoría no existe en programación local y es la que define a los sistemas
distribuidos.

### Plano de control y plano de datos

El **middleware** mantiene el registro de servicios y monitorea salud, pero ninguna
petición de usuario pasa por él. El **load balancer** está en el camino crítico de cada
petición y solo decide a qué instancia le toca. Es la misma separación que hacen
Consul/Envoy o el API server y kube-proxy en Kubernetes.

Que la separación es real se comprueba apagando el middleware: el balanceador de Go sigue
enrutando con su caché de instancias sanas. Pierde la capacidad de detectar caídas nuevas,
no la de servir. nginx ni se entera, porque sus upstreams son estáticos.

### Instancias sin estado

Ninguna instancia guarda datos en memoria. Todo el estado vive en PostgreSQL (también los
avisos y la lista de espera), y por eso da igual cuál de las tres atienda una petición. Sin
esa propiedad, el balanceo sería imposible. La posición de cada consumidor en Kafka
tampoco vive en la instancia: el offset confirmado lo guarda Kafka, por eso otra instancia
puede seguir exactamente donde se quedó la que se apagó.

---

## Pruebas

```bash
./pruebas.sh                 # gateways, balanceo, failover, validaciones
./pruebas_kafka.sh           # mensajería asíncrona y lista de espera (ver sección de Kafka)
./simulacro.sh               # "día de inscripciones": concurrencia de verdad
./verificar_consistencia.sh  # ¿cuadran los datos entre las bases?
```

### Simulacro de día de inscripciones

`simulacro.sh` usa el grupo SIM100-01 (Taller de Concurrencia, 10 lugares), que existe solo
para esto, y por defecto 40 alumnos nuevos:

1. Los 40 piden el grupo **al mismo tiempo**. Deben entrar exactamente 10, ni uno más
   (`SELECT ... FOR UPDATE`), y el script muestra cómo se repartieron las peticiones entre
   las 3 instancias de inscripciones.
2. Los 30 que no alcanzaron se forman en la lista de espera, en posiciones 1 a 30.
3. 4 de los inscritos se dan de baja. Por Kafka, la fila avanza sola, y el script comprueba
   que entraron **los 4 primeros de la fila**, en ese orden, y que el grupo volvió a 10/10.
4. Corre `verificar_consistencia.sh` y deja el grupo vacío para poder repetirlo.

Con otro número de alumnos: `./simulacro.sh 80`.

### Verificador de consistencia

Cada servicio tiene su propia base y entre bases no hay transacciones: lo único que las
mantiene de acuerdo son las llamadas HTTP y los eventos. `verificar_consistencia.sh` lo
comprueba con los datos reales:

1. Kafka no tiene eventos pendientes (si los hay, espera hasta 20 s a que se procesen)
2. El cupo ocupado de cada grupo, en `materias_db`, es igual al número de inscripciones
   activas de ese grupo, en `inscripciones_db`
3. Ningún grupo pasa de su cupo
4. Nadie lleva dos grupos de la misma materia
5. Nadie está inscrito y formado a la vez en el mismo grupo

Sirve después de cualquier prueba, o después de apagar y prender instancias a mano a la
mitad de una operación. Si algo no cuadra, dice exactamente qué grupo y por cuánto.

### pruebas.sh

`pruebas.sh` recorre once escenarios con pausas entre cada uno:

1. Estado del registro de service discovery
2. Round robin por nginx sobre las 3 instancias de `alumnos`
3. Pools independientes: `materias`, `grupos` e `inscripciones` con cursores propios
4. Las dos implementaciones del gateway, lado a lado
5. **Chequeo pasivo (nginx) contra chequeo activo (middleware)**
6. Recuperación automática de una instancia
7. El middleware fuera del camino crítico
8. Los seis eslabones de la cadena de validaciones
9. **Tráfico entre servicios visible en los logs de nginx**
10. 10 peticiones concurrentes al último lugar de un grupo lleno
11. Registro dinámico, y su límite con upstreams estáticos

Los escenarios 5 y 9 son los que ilustran el valor de haber añadido nginx.

### Verificación manual del balanceo

```bash
for i in $(seq 1 9); do
  curl -s -D - -o /dev/null http://localhost:8000/alumnos | grep -i "^x-instance-id"
done
```

Debe rotar entre `alumnos-1`, `alumnos-2` y `alumnos-3`. Cambia el puerto a 8001 para ver
el balanceador de Go haciendo lo mismo.

### Verificación del failover

```bash
docker compose stop materias-2
sleep 7
for i in $(seq 1 6); do curl -s -o /dev/null -w "%{http_code} " http://localhost:8000/materias; done
echo
for i in $(seq 1 6); do curl -s -o /dev/null -w "%{http_code} " http://localhost:8001/materias; done
docker compose start materias-2
```

Por nginx salen uno o dos `502`; por Go, seis `200`.

---

## Datos de prueba

La base arranca con **204 alumnos, 13 materias, 22 grupos y 391 inscripciones**. Los 4
alumnos, 4 materias y 6 grupos originales siguen igual, con los mismos ids, así que
`pruebas.sh` y los ejemplos de siempre funcionan como antes.

Lo demás lo genera `db/generar_datos.py` (`python3 db/generar_datos.py` vuelve a escribir
los archivos `10-`, `11-` y `12-` de `db/init`). Es determinístico: con la misma semilla
genera siempre lo mismo. Lo importante es que **los lugares ocupados de cada grupo salen
de contar las inscripciones sembradas**, así que el sistema arranca consistente y
`verificar_consistencia.sh` pasa desde el principio. Las inscripciones sembradas respetan
las mismas reglas que la cadena: sin empalmes, con prerrequisitos y dentro del límite de
créditos.

Casos preparados a propósito:

- El **grupo 02 de TC2027** está lleno (2 de 2): rechazo por cupo y lista de espera
- El **grupo 01 de TC1028** y el **grupo 01 de TC2027** son ambos lunes por la mañana:
  rechazo por empalme
- El **grupo 02 de TC1028** está en 29 de 30: una inscripción más lo llena y publica
  `grupos.llenos`
- **Prerrequisitos**, solo en materias nuevas: TC2005 y TC3050 piden TC1028; MA2002 e
  IN2001 piden MA1001; TC4010 pide TC1010
- Los alumnos originales no tienen inscripciones, para que las demos empiecen limpias, pero
  sí materias aprobadas (ver [Cómo se usa](#cómo-se-usa))
- **SIM100-01**, vacío y en sábado, solo para `simulacro.sh`

---

## Limitaciones conocidas

- **Doble escritura.** Los servicios guardan en su base y *después* publican en Kafka. Si
  el servicio muere entre las dos cosas, o Kafka no responde, el cambio queda guardado y el
  evento puede no salir: queda un `ADVERTENCIA` en el log. (Si Kafka regresa en uno o dos
  segundos, el productor de kafka-go todavía lo entrega por su cuenta; si tarda más, se
  pierde.) En una baja, eso significa un lugar que no se devuelve. La solución estándar es el patrón *outbox*: escribir el evento en una
  tabla de la misma base, en la misma transacción, y que otro proceso lo publique.
- **Eventos descartados.** Si un handler falla 3 veces seguidas, el evento se registra en
  el log y se confirma para no detener la cola. En producción iría a un tópico de "cartas
  muertas" (dead letter queue) para revisarlo a mano.
- **La fila se puede atorar.** Si se pierde un `lugares.liberados` (doble escritura en
  `materias`, o un evento descartado tras 3 fallos), ese lugar queda libre pero nadie de la
  fila entra, y por la regla `validarSinFila` tampoco lo toma nadie directo. Se destraba con
  la siguiente baja del grupo, que vuelve a intentar con la fila completa.
- **Los avisos no expiran.** `notificaciones_db` crece sin límite; la API devuelve solo los
  50 más recientes.
- **El periodo de inscripción es por configuración**, igual para todos, y en hora UTC (la
  de los contenedores). No hay periodos por carrera ni por semestre.
- **Un solo broker, sin réplicas y sin volumen.** Si el contenedor de Kafka se cae, nadie
  puede publicar ni consumir hasta que regresa, y `docker compose down` borra los eventos
  (`kafka-init` vuelve a crear los tópicos vacíos al levantar). En producción serían 3
  brokers con `replication-factor 3` y almacenamiento persistente.
- **Upstreams estáticos en nginx.** nginx no lee el registro de Consul: sus pools están
  escritos en el `nginx.conf`. El siguiente paso sería `consul-template`, que regenera la
  configuración cuando el registro cambia, como en `consul_microservices` de la clase.
- **Re-registro en Consul.** Si Consul se reinicia, las instancias no se vuelven a
  registrar solas hasta reiniciarse. En producción se resuelve con un agente de Consul en
  cada nodo.
- **El hueco del Saga** descrito en [Acción compensatoria](#acción-compensatoria).

---

## Solución de problemas

| Síntoma | Causa y solución |
|---|---|
| Solo arrancan 4 contenedores y hay WARN de `POSTGRES_*` | Falta el `.env`. Está en el `.gitignore`, así que no viaja con el clon: corre `./setup.sh`, o `cp .env.example .env` |
| `docker --version` dice "Emulate Docker CLI using podman" | Es el shim de Podman: `sudo dnf remove podman-docker` y reinstala Docker |
| Podman intenta descargar `inscripciones/alumnos` de un registro | Nombre corto sin registro. Usa Docker, o prefija las imágenes con `localhost/` |
| `lookup registry-1.docker.io ... no such host` al bajar una imagen | El DNS que entrega la red no responde. Pon `nameserver 8.8.8.8` en `/etc/resolv.conf`, y para que NetworkManager no lo vuelva a cambiar, agrega `dns=none` en la sección `[main]` de un archivo en `/etc/NetworkManager/conf.d/` |
| `permission denied` en un volumen, con permisos correctos | SELinux. Verifica que los bind mounts lleven `:z`. Diagnostica con `sudo ausearch -m avc -ts recent` |
| nginx no arranca: `host not found in upstream` | Alguna instancia de backend no está corriendo |
| nginx responde 502, o 404 en rutas que sí existen, después de recrear o reiniciar servicios | nginx resolvió los nombres de los backends al arrancar, y esas IPs ahora son de otro contenedor (o de ninguno): `docker compose restart nginx-gateway`. `setup.sh` y `pruebas_kafka.sh` ya lo hacen solos |
| Cambié una variable de entorno y no pasó nada | Hay que recrear el contenedor: `docker compose up -d --force-recreate <servicio>` |
| `"sanas": 0` en `/status` | Espera 10s al primer ciclo de heartbeat |
| Cambios en los `.sql` no se aplican | Los scripts de init solo corren con el volumen vacío: `docker compose down -v` |
| El frontend carga pero sin datos | Revisa CORS en la consola del navegador y recarga con Ctrl+Shift+R |
| `dependency failed to start: container kafka-init exited (1)` | Kafka no creó los tópicos. Mira por qué con `docker compose logs kafka-init`, y vuelve a intentar con `docker compose up -d` |
| El contenedor `kafka` se reinicia o nunca queda `healthy` | Casi siempre es memoria. Revisa `docker compose logs kafka` y libera RAM; el tope de la JVM está en `KAFKA_HEAP_OPTS` |
| `Unknown Topic Or Partition` en el log de un servicio | El tópico no existe. Corre `docker compose up kafka-init` y revisa que termine con `exited (0)` |
| `describe --group` dice que el grupo se está rebalanceando | Una instancia entró o salió hace un momento. Espera unos segundos y vuelve a preguntar |
| `column "baja_en" does not exist`, `relation "lista_espera" does not exist`, o `simulacro.sh` dice que no existe SIM100 | La base es de una versión anterior: `./setup.sh --limpio` (borra los datos y siembra los nuevos) |
| `verificar_consistencia.sh` dice que un grupo no cuadra justo después de una prueba | Si dice que hay eventos pendientes, espera unos segundos y córrelo otra vez. Si sigue sin cuadrar, revisa `docker compose logs` buscando `ADVERTENCIA`: es un evento que no salió (ver Limitaciones) |
| El frontend no muestra los avisos | Revisa que `notificaciones-1` y `-2` estén arriba y que `curl localhost:8000/notificaciones?matricula=A01234567` conteste |

---

## Stack

| Capa | Tecnología |
|---|---|
| Gateway | nginx (Alpine), configuración declarativa |
| Backend | Go 1.22. Dependencias: `lib/pq` (PostgreSQL), `segmentio/kafka-go` (Kafka), `google/uuid` (ids de eventos) |
| Service discovery | Consul (modo dev) |
| Mensajería | Apache Kafka 3.8, modo KRaft, un broker |
| Base de datos | PostgreSQL 16 (Alpine) |
| Frontend | React 18 + Vite, un archivo de estilos propio, servido por Nginx |
| Contenedores | Docker + Docker Compose v2 |

Las imágenes de los servicios usan build multi-etapa con `CGO_ENABLED=0`, lo que produce
binarios estáticos e imágenes finales de aproximadamente 15 MB. El contenedor de nginx son
tres líneas de Dockerfile: ahí está, en forma concreta, el intercambio entre escribir un
proxy y configurar uno.
