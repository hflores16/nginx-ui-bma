# NGINX UI BMA - Dashboard de Balanceo

Esta personalización parte del commit oficial de NGINX UI **v2.5.10**:

```
ff2253c59cdbfc0741866cd66a1f147ad8292791
```

## Objetivo

Agregar una página **Balanceo** dentro de la misma interfaz de NGINX UI para visualizar:

- clientes activos por backend;
- distribución porcentual por nodo;
- requests y requests/segundo;
- latencia promedio del upstream;
- tiempo promedio de conexión;
- respuestas 4XX y 5XX;
- tráfico transferido;
- health status de los upstreams ya monitoreados por NGINX UI;
- histórico de requests por backend;
- filtros por servicio, puerto y período.

No requiere Grafana, Loki ni Prometheus.

## Fuente de métricas

Por defecto el backend lee:

```
/var/log/nginx/metrics/all.metrics.log
```

Se puede cambiar sin recompilar:

```bash
export NGINX_UI_BMA_METRICS_PATH=/otra/ruta/all.metrics.log
```

El log esperado es JSON Lines, por ejemplo:

```json
{"timestamp":"2026-09-25T09:39:58-04:00","lb":"bmanlb1.bancoademi.local","client":"172.23.10.84","host":"pkmcls.bancoademi.local","server_port":"443","method":"POST","uri":"/filesflowbe/Process/taskcase/get","status":"200","bytes":"341","upstream":"172.25.112.168:80","upstream_status":"200","upstream_connect":"0.000","upstream_header":"0.022","upstream_response":"0.022","request_time":"0.023","request_id":"..."}
```

## API

La interfaz consume:

```
GET /api/bma/metrics
```

Parámetros:

- `period`: `1m`, `5m`, `15m`, `1h`, `6h`, `24h`
- `service`: host/servicio normalizado, por ejemplo `pkmcls`
- `port`: puerto de backend, por ejemplo `8078`

La API requiere la misma autenticación que el resto de las rutas privadas de NGINX UI.

## Build

El workflow:

```
.github/workflows/bma-build.yml
```

genera el artifact:

```
nginx-ui-bma-linux-amd64
```

Incluye frontend y backend en un único ejecutable, igual que NGINX UI original.

## Prueba paralela en BMANLB1

No sustituir producción inicialmente.

Producción:

```
127.0.0.1:9000 -> /usr/local/bin/nginx-ui
```

Prueba:

```
127.0.0.1:9001 -> /usr/local/bin/nginx-ui-bma
```

La prueba debe usar una copia del `app.ini` y, preferiblemente, una copia de la base de datos de NGINX UI para evitar que dos instancias escriban simultáneamente sobre la misma SQLite.

El acceso desde una estación Windows puede hacerse sin abrir un puerto adicional:

```powershell
ssh -N -L 9001:127.0.0.1:9001 infradmin@172.25.114.222
```

y luego:

```
http://127.0.0.1:9001
```

## Seguridad

- El path del log no se recibe desde el navegador.
- La API no devuelve URI ni Request ID de las peticiones.
- Solo se exponen agregados de observabilidad.
- Las rutas están bajo `AuthRequired()`.
- No se modifican upstreams, pesos, sticky sessions ni `proxy_pass`.
