'use strict';

const stateNames = ['Sin muestra', 'Sin confirmar', 'Operativo', 'Degradado', 'Interrupción'];
const healthNames = { UP: 'Responde', DOWN: 'Con problemas', UNREACHABLE: 'Sin respuesta', UNKNOWN: 'Sin confirmar' };
const componentNames = { database: 'Base de datos', rabbitmq: 'RabbitMQ', mqtt: 'MQTT', configuration: 'Configuración' };
const date = new Intl.DateTimeFormat('es-PE', { timeZone: 'America/Lima', dateStyle: 'medium', timeStyle: 'medium' });
const time = new Intl.DateTimeFormat('es-PE', { timeZone: 'America/Lima', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
const seconds = new Intl.DateTimeFormat('es-PE', { timeZone: 'America/Lima', timeStyle: 'medium', hourCycle: 'h23' });
const percentage = new Intl.NumberFormat('es-PE', { style: 'percent', maximumFractionDigits: 1 });
const get = id => document.getElementById(id);
const rows = new Map();
let busy = false;

function setText(element, value) {
  if (element.textContent !== value) element.textContent = value;
}

const healthy = service => service.readiness.status === 'UP' && service.liveness.status === 'UP';

function statusFor(service) {
  if (healthy(service)) return service.instances?.some(instance => !healthy(instance)) ? 3 : 2;
  if ([service.readiness.status, service.liveness.status].includes('UNKNOWN')) return 1;
  return 4;
}

function renderService(id, name, state, label, timeline, details, after = null) {
  let row = rows.get(id);
  if (!row) {
    const element = get('service-template').content.firstElementChild.cloneNode(true);
    element.id = id;
    element.classList.toggle('instance', Boolean(after));
    element.querySelector('h3').textContent = name;
    element.querySelector('details').hidden = details.length === 0;
    const track = element.querySelector('.track');
    row = {
      element, track, badge: element.querySelector('.state-label'),
      availability: element.querySelector('.availability'), diagnostic: element.querySelector('.diagnostic')
    };
    rows.set(id, row);
    if (after) after.after(element);
    else get('services').append(element);
  }
  row.badge.dataset.state = state;
  setText(row.badge, label);
  const series = timeline.rows[id];
  const value = series?.observed ? percentage.format(series.available / series.observed) : '—';
  setText(row.availability, value);
  row.availability.title = series?.observed ? series.available + ' de ' + series.observed + ' comprobaciones disponibles' : 'Sin comprobaciones confirmadas';
  const states = series?.states || '0'.repeat(30);
  row.track.setAttribute('aria-label', name + ' · ' + row.availability.title);
  const start = Date.parse(timeline.start);
  const step = timeline.stepSeconds * 1000;
  if (row.track.childElementCount !== states.length) {
    row.track.replaceChildren(...Array.from(states, () => document.createElement('span')));
  }
  for (let index = 0; index < states.length; index++) {
    const cell = row.track.children[index];
    if (cell.dataset.state === states[index] && row.start === start) continue;
    const from = start + index * step;
    cell.dataset.state = states[index];
    cell.title = seconds.format(from) + '–' + seconds.format(from + step) + ' · ' + stateNames[Number(states[index])];
  }
  row.start = start;

  const diagnostic = details.join('\n');
  if (row.details !== diagnostic) {
    row.diagnostic.replaceChildren(...details.map(value => {
      const line = document.createElement('p');
      line.textContent = value;
      return line;
    }));
    row.details = diagnostic;
  }
  return row.element;
}

function detailsFor(service) {
  const ready = service.readiness;
  const details = [
    'Proceso · ' + (healthNames[service.liveness.status] || 'Sin confirmar'),
    (service.basic ? 'Solo se comprueba el proceso del Gateway' : 'Disponibilidad · ' + (healthNames[ready.status] || 'Sin confirmar')),
    (ready.httpStatus ? 'HTTP ' + ready.httpStatus : 'Sin conexión HTTP') + ' · ' + ready.durationMs + ' ms · ' + service.endpoint
  ];
  for (const [key, component] of Object.entries(ready.components || {})) {
    const name = component.details?.type || componentNames[key] || key;
    details.push(name + ' · ' + (healthNames[component.status] || 'Sin confirmar'));
  }
  if (ready.message) details.push(ready.message);
  return details;
}

function render(snapshot) {
  const timeline = snapshot.timeline;
  const systemState = { UP: 2, DEGRADED: 3, DOWN: 4 }[snapshot.status] || 1;
  const systemTitle = {
    UP: 'Todos los servicios operativos',
    DEGRADED: 'Servicio disponible con redundancia reducida',
    DOWN: 'Hay servicios que requieren atención'
  }[snapshot.status] || 'Estado del sistema sin confirmar';
  for (const [id, row] of rows) {
    if (timeline.rows[id]) continue;
    row.element.remove();
    rows.delete(id);
  }
  const latest = new Date(snapshot.checkedAt);
  const count = snapshot.services.filter(healthy).length;
  const notificationInstances = snapshot.services.find(service => service.id === 'notifications')?.instances || [];
  let summary = count + ' de ' + snapshot.services.length + ' servicios disponibles';
  if (notificationInstances.length) {
    summary += ' · Notifications ' + notificationInstances.filter(healthy).length + '/' + notificationInstances.length + ' VMs operativas';
  }
  document.querySelector('.overview').dataset.state = systemState;
  setText(get('overall'), systemTitle);
  setText(get('summary'), summary);
  get('checked').dateTime = snapshot.checkedAt;
  setText(get('checked'), date.format(latest));
  get('error').hidden = true;

  const from = Date.parse(timeline.start);
  const to = from + timeline.rows.system.states.length * timeline.stepSeconds * 1000;
  const axis = get('axis-times');
  if (axis.dataset.start !== timeline.start) {
    axis.replaceChildren(...[from, (from + to) / 2, to].map(value => {
      const tick = document.createElement('span');
      tick.textContent = time.format(value);
      return tick;
    }));
    axis.dataset.start = timeline.start;
  }

  renderService('system', 'Sistema', systemState, stateNames[systemState], timeline, []);
  for (const service of snapshot.services) {
    const status = statusFor(service);
    let previous = renderService(service.id, service.name, status, stateNames[status], timeline, detailsFor(service));
    for (const instance of service.instances || []) {
      const instanceStatus = statusFor(instance);
      previous = renderService(instance.id, service.name + ' · ' + instance.name, instanceStatus, stateNames[instanceStatus], timeline, detailsFor(instance), previous);
    }
  }
}

function refreshLabel() {
  return get('automatic').checked ? 'Actualización automática · 1 s' : 'Actualización pausada';
}

async function refresh() {
  if (busy) return;
  busy = true;
  try {
    const response = await fetch('/status/services?history=1', { cache: 'no-store', signal: AbortSignal.timeout(5000) });
    if (!response.ok) throw new Error('Panel no disponible');
    const snapshot = await response.json();
    if (!snapshot.timeline?.rows.system || !Array.isArray(snapshot.services) || !snapshot.services.length || !Number.isFinite(Date.parse(snapshot.checkedAt))) {
      throw new Error('Respuesta incompleta');
    }
    render(snapshot);
    setText(get('refresh-state'), refreshLabel());
  } catch {
    document.querySelector('.overview').dataset.state = 1;
    setText(get('overall'), 'Estado actual sin confirmar');
    setText(get('refresh-state'), 'No se pudo actualizar');
    setText(get('error'), 'El panel no respondió. Si hay datos visibles, corresponden a la última comprobación recibida.');
    get('error').hidden = false;
  } finally {
    busy = false;
  }
}

get('refresh').addEventListener('click', refresh);
get('automatic').addEventListener('change', () => {
  setText(get('refresh-state'), refreshLabel());
  if (get('automatic').checked) refresh();
});
const refreshIfVisible = () => { if (!document.hidden && get('automatic').checked) refresh(); };
document.addEventListener('visibilitychange', refreshIfVisible);
setInterval(refreshIfVisible, 1000);
refresh();
