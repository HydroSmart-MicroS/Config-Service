'use strict';

const labels = { UP: 'Operativo', DOWN: 'Con problemas', UNREACHABLE: 'Sin respuesta', UNKNOWN: 'Estado desconocido' };
const descriptions = {
  identity: 'Cuentas y autenticación', properties: 'Unidades, medidores y asignaciones',
  telemetry: 'Lecturas de consumo y conexión MQTT', analytics: 'Consumo, metas y alertas',
  reports: 'Generación de reportes', notifications: 'Notificaciones y procesamiento de correos',
  billing: 'Planes y pagos', 'config-service': 'Configuración central de los servicios', gateway: 'Entrada HTTP de la aplicación'
};
const componentNames = { database: 'Base de datos', rabbitmq: 'RabbitMQ', mqtt: 'MQTT', configuration: 'Configuración' };
const get = id => document.getElementById(id);
let busy = false;
let ending = false, idleUntil = 0, idleTimer, activityTimer, nextActivity = 0;
const hasSession = Boolean(get('status-logout'));

function endSession() {
  ending = true;
  get('services').replaceChildren();
  location.replace('/status/login?expired=1');
}

function trackIdle(response, started) {
  if (!hasSession) return;
  const remaining = response.headers.get('X-Status-Idle-Ms');
  if (remaining === null) return;
  const deadline = started + Number(remaining);
  if (deadline <= idleUntil) return;
  idleUntil = deadline;
  clearTimeout(idleTimer);
  idleTimer = setTimeout(endSession, Math.max(0, deadline - Date.now()));
}

function noteActivity(event) {
  if (!event.isTrusted || ending || document.hidden || activityTimer) return;
  activityTimer = setTimeout(async () => {
    activityTimer = undefined;
    if (ending || document.hidden) return;
    const started = Date.now();
    nextActivity = started + 5000;
    try {
      const response = await fetch('/status/activity', { method: 'POST', cache: 'no-store', signal: AbortSignal.timeout(8000) });
      if (response.status === 401) endSession();
      else if (response.ok) trackIdle(response, started);
    } catch {
      // The last confirmed idle deadline still applies during a connection error.
    }
  }, Math.max(0, nextActivity - Date.now()));
}

function render(snapshot) {
  const openDetails = new Set([...document.querySelectorAll('.service details[open]')].map(item => item.closest('.service').id));
  const cards = document.createDocumentFragment();
  for (const service of snapshot.services) {
    const card = get('service-template').content.firstElementChild.cloneNode(true);
    card.id = service.id;
    card.querySelector('h2').textContent = service.name;
    const status = service.readiness.status === 'UP' ? service.liveness.status : service.readiness.status;
    const badge = card.querySelector('.badge');
    badge.textContent = labels[status] || labels.UNKNOWN;
    badge.classList.add(status.toLowerCase());
    card.querySelector('.service-description').textContent = descriptions[service.id] || '';
    card.querySelector('.live').textContent = service.liveness.status === 'UP' ? 'Responde' : labels[service.liveness.status] || labels.UNKNOWN;
    card.querySelector('.ready').textContent = service.basic ? 'Solo proceso' : service.readiness.status === 'UP' ? 'Sí' : service.readiness.status === 'DOWN' ? 'No' : labels[service.readiness.status] || labels.UNKNOWN;
    const dependencies = card.querySelector('.dependencies');
    const detailList = card.querySelector('.component-details');
    for (const [name, component] of Object.entries(service.readiness.components || {})) {
      const label = component.details.type || componentNames[name] || name;
      const chip = document.createElement('span');
      chip.className = 'dependency ' + component.status.toLowerCase();
      chip.textContent = label + ': ' + (component.status === 'UP' ? 'OK' : component.status === 'DOWN' ? 'Falla' : 'Sin datos');
      dependencies.append(chip);
      const item = document.createElement('li');
      item.textContent = label + ' · ' + (labels[component.status] || labels.UNKNOWN) + ' · ' + Math.round(component.details.durationMs || 0) + ' ms';
      detailList.append(item);
    }
    card.querySelector('.endpoint').textContent = 'Readiness: ' + service.endpoint;
    card.querySelector('.diagnostic').textContent = service.readiness.message || 'Todas las comprobaciones requeridas respondieron correctamente.';
    card.querySelector('.duration').textContent = 'Readiness · ' + service.readiness.durationMs + ' ms';
    card.querySelector('.http').textContent = service.readiness.httpStatus ? 'HTTP ' + service.readiness.httpStatus : 'Sin conexión HTTP';
    card.querySelector('details').open = openDetails.has(service.id);
    cards.append(card);
  }
  get('services').replaceChildren(cards);
  const healthy = snapshot.services.filter(service => service.readiness.status === 'UP' && service.liveness.status === 'UP').length;
  const good = healthy === snapshot.services.length;
  document.querySelector('.overview').classList.toggle('issue', !good);
  document.querySelector('.overview').classList.remove('stale');
  get('overall').textContent = good ? 'Todos los servicios responden' : 'Hay servicios que requieren atención';
  get('summary').textContent = healthy + ' de ' + snapshot.services.length + ' comprobaciones correctas · 8 servicios y API Gateway.';
  get('checked').textContent = new Intl.DateTimeFormat('es-PE', { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(snapshot.checkedAt));
  get('error').hidden = true;
}

async function refresh() {
  if (busy || ending) return;
  busy = true;
  get('refresh').disabled = true;
  get('refresh').textContent = 'Comprobando…';
  get('refresh-state').textContent = 'Consultando los servicios…';
  try {
    const started = Date.now();
    const response = await fetch('/status/services', { cache: 'no-store', signal: AbortSignal.timeout(8000) });
    if (hasSession && response.status === 401) {
      endSession();
      return;
    }
    if (!response.ok) throw new Error('No se pudo consultar Config Service');
    trackIdle(response, started);
    const snapshot = await response.json();
    if (!Array.isArray(snapshot.services) || !snapshot.services.length) throw new Error('Respuesta incompleta');
    render(snapshot);
    get('refresh-state').textContent = get('automatic').checked ? 'Actualización automática cada 15 s' : 'Actualización automática pausada';
  } catch {
    document.querySelector('.overview').classList.add('stale');
    get('overall').textContent = 'No se pudo actualizar el estado';
    get('summary').textContent = 'El estado actual de los servicios no está confirmado.';
    get('refresh-state').textContent = 'Última actualización fallida';
    get('error').textContent = 'No hay conexión con el panel de Config Service. Si ya había resultados, corresponden a la última comprobación correcta. Revisa que Config Service esté encendido y vuelve a actualizar.';
    get('error').hidden = false;
  } finally {
    busy = false;
    get('refresh').disabled = false;
    get('refresh').textContent = 'Actualizar ahora';
  }
}

if (hasSession) {
  for (const type of ['pointerdown', 'pointermove', 'keydown', 'wheel']) {
    document.addEventListener(type, noteActivity, { passive: true });
  }
  window.addEventListener('pageshow', event => {
    if (event.persisted) {
      get('services').replaceChildren();
      refresh();
    }
  });
}
get('refresh').addEventListener('click', refresh);
get('automatic').addEventListener('change', () => {
  get('refresh-state').textContent = get('automatic').checked ? 'Actualización automática cada 15 s' : 'Actualización automática pausada';
  if (get('automatic').checked) refresh();
});
document.addEventListener('visibilitychange', () => { if (!document.hidden && get('automatic').checked) refresh(); });
setInterval(() => { if (!document.hidden && get('automatic').checked) refresh(); }, 15000);
refresh();
