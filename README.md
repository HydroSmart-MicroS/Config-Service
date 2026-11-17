# Config Service · Go

Valores no sensibles en config/local/*.json. Los siete servicios cargan su configuración mediante RabbitMQ RPC al arrancar;
las variables de entorno explícitas tienen prioridad. No hay actualización en caliente ni secretos en sus respuestas.

Gateway usa infra/gateway/upstreams.env, generado por Initialize-Local desde config/local/gateway.json.
El servicio atiende RPC y su HTTP interno de salud; las rutas de configuración no se publican por el Gateway.

## Panel local de estado

Abre http://127.0.0.1:8098/ o `/status`. Consulta los siete microservicios de negocio,
el propio Config Service y el Gateway. El navegador se actualiza cada segundo y permite pausar o consultar manualmente.
El panel muestra la línea de tiempo de los últimos 15 minutos, además del diagnóstico de proceso (liveness), disponibilidad (readiness) y dependencias.
Con NOTIFICATIONS_REPLICA_ENABLED=true, Notifications mantiene su estado agregado y muestra las filas de la VM principal y la réplica por separado.
Una dependencia caída aparece como problema; un servicio inaccesible, como «Sin respuesta».
El Gateway solo tiene comprobación de proceso y se indica expresamente.

El JSON agregado está en `/status/services` (HTTP 200 para una consulta del panel correcta;
su campo `status` indica UP, DEGRADED o DOWN). Añade `?history=1` para incluir la línea de tiempo. `/health/ready` conserva la salud
propia de Config Service: la caída de otro microservicio no cambia ese endpoint.

Las consultas de diagnóstico usan HTTP directo dentro de Docker, para poder seguir mostrando
las demás dependencias si RabbitMQ falla. La comunicación de negocio por RabbitMQ se conserva.
Config Service comparte un muestreo de solo lectura entre todos los navegadores. Lo intenta cada segundo y espera hasta 5 segundos por comprobación sin superponer ciclos. La línea de tiempo se guarda en memoria y se reinicia con el proceso. El porcentaje se calcula solo con muestras confirmadas. Si el panel deja de responder, conserva la última comprobación recibida y avisa que el estado actual no está confirmado.

El puerto 8098 está publicado solo en 127.0.0.1 en `compose.local.yaml`; no hace falta
el perfil de demostración ni publicar los puertos de los otros servicios para usar el panel.

[Arranque y funciones](../README.md) · [Colas](../docs/rabbitmq-guide.md).

## Acceso al panel en MAIN

Con Neon y Atlas, abre [Config Service](http://127.0.0.1:8099/) o el [acceso local desde Gateway](http://localhost:8081/status). Ambos muestran el estado de nueve componentes. El JSON de esta vista está en `/status/services`. Para verificar la configuración y las dependencias de los servicios juntos, ejecuta `./scripts/Test-Local.ps1 -DatabaseMode Hosted` desde la carpeta de orquestación.
