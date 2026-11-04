# Config Service · Go

Valores no sensibles en config/local/*.json. Los siete servicios cargan su configuración mediante RabbitMQ RPC al arrancar;
las variables de entorno explícitas tienen prioridad. No hay actualización en caliente ni secretos en sus respuestas.

Gateway usa infra/gateway/upstreams.env, generado por Initialize-Local desde config/local/gateway.json.
El servicio atiende RPC y su HTTP interno de salud; las rutas de configuración no se publican por el Gateway.

## Panel local de estado

Abre http://127.0.0.1:8098/ o `/status`. Consulta los siete microservicios de negocio,
el propio Config Service y el Gateway, con actualización cada 15 segundos y botón manual.
Muestra proceso (liveness), disponibilidad (readiness), dependencias, duración y fecha.
Una dependencia caída aparece como problema; un servicio inaccesible, como «Sin respuesta».
El Gateway solo tiene comprobación de proceso y se indica expresamente.

El JSON agregado está en `/status/services` (HTTP 200 para una consulta del panel correcta;
su campo `status` es DOWN si alguna comprobación falla). `/health/ready` conserva la salud
propia de Config Service: la caída de otro microservicio no cambia ese endpoint.

Las consultas de diagnóstico usan HTTP directo dentro de Docker, para poder seguir mostrando
las demás dependencias si RabbitMQ falla. La comunicación de negocio por RabbitMQ se conserva.
Destinos fijos, consultas paralelas de solo lectura con límite de 5 segundos, sin secretos,
sin historial, sin pagos ni correos reales. Si Config Service no responde, la página avisa
que los resultados anteriores ya no confirman el estado actual.

El puerto 8098 está publicado solo en 127.0.0.1 en `compose.local.yaml`; no hace falta
el perfil de demostración ni publicar los puertos de los otros servicios para usar el panel.

[Arranque y funciones](../README.md) · [Colas](../docs/rabbitmq-guide.md).
