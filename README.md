# netint_vpu_exporter

`netint_vpu_exporter` es un Exporter para Prometheus que recolecta métricas de dispositivos Netint VPU y las expone a través de un endpoint HTTP.

## Características

- Exporta métricas de decodificadores, codificadores, escaladores y AIs de Netint.
- Incluye métricas como carga, memoria, instancias, temperatura, etc.
- Utiliza [zerolog](internal/config/zerolog.go) para logging configurable.
- Compatible con Prometheus.

## Instalación

### Requisitos

- Go 1.24+
- Acceso a los binarios `ni_rsrc_mon` y `nvme` en el PATH.

### Compilación

```sh
make build
```

Esto generará los binarios `netint_vpu_exporter_arm` y `netint_vpu_exporter_x64`.

## Uso

```sh
./netint_vpu_exporter_x64 --web.listen-address=":9836" --log.level="info"
```

- `--web.listen-address`: Dirección y puerto donde se expondrá el endpoint `/metrics`. Por defecto `:9836`.
- `--log.level`: Nivel de log (`debug`, `info`, `warn`, `error`). Por defecto `info`.

## Endpoint de métricas

El Exporter expone las métricas en:

```
http://localhost:9836/metrics
```

## Variables de entorno

Puedes configurar el nivel de log y el puerto mediante flags de línea de comandos.

## Ejemplo de métrica expuesta

```
# HELP netint_decoder_LOAD_total Total LOAD for decoder
# TYPE netint_decoder_LOAD_total gauge
netint_decoder_LOAD_total{device="nvme0n1",index="0"} 10
```

## Licencia

MIT. Ver [LICENSE](LICENSE).

---