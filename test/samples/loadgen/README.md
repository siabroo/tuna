# loadgen

Configurable CPU + memory load generator used as a tuna e2e fixture.

## HTTP API

- `GET /load` → `{"cpu_percent": 30, "mem_mb": 40}`
- `POST /load` body `{"cpu_percent": 50, "mem_mb": 128}` → set both
- `POST /load/cpu?percent=N` → set CPU only
- `POST /load/mem?mb=N` → set memory only
- `DELETE /load` → idle (cpu=0, mem=0)
- `GET /metrics` → Prometheus
- `GET /healthz` → liveness

Initial load via env: `LOAD_CPU_PERCENT`, `LOAD_MEM_MB`.

## Example

~~~bash
kubectl port-forward deployment/loadgen 8080:8080 &
curl http://localhost:8080/load                                   # current
curl -X POST -d '{"cpu_percent":80,"mem_mb":128}' http://localhost:8080/load
curl -X DELETE http://localhost:8080/load                         # release
~~~
