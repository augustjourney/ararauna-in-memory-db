# ararauna

[![CI](https://github.com/augustjourney/ararauna-in-memory-db/actions/workflows/ci.yml/badge.svg)](https://github.com/augustjourney/ararauna-in-memory-db/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/augustjourney/ararauna-in-memory-db)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/augustjourney/ararauna-in-memory-db)](https://goreportcard.com/report/github.com/augustjourney/ararauna-in-memory-db)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](#лицензия)

In-memory key-value база данных на Go. Совместима с `redis-cli` через RESP-протокол.

> **Учебный проект.** Создаю для изучения внутреннего устройства баз данных: write-ahead logging, репликация, конкурентные структуры данных, дизайн сетевых протоколов. Без намерений использовать в production.

## Что реализовано

- **TCP-сервер с RESP2-протоколом** — простые/bulk-строки, ошибки, integers, массивы. Работает с `redis-cli` и любым Redis-клиентом
- **Команды**: `PING`, `GET`, `SET`, `DEL`
- **Шардированная in-memory map** с `RWMutex` на шард
- **TTL-инфраструктура**:
  - значение хранит `ExpiresAt *time.Time` (nullable)
  - ленивая инвалидация при `GET`
  - фоновый GC: один шард за тик, бюджет времени на проход
  - в WAL записывается отдельный опкод `SETX` с `expires_unix_nano`
  - *(пользовательские команды `EXPIRE` / `SET ... EX` пока не выставлены на RESP)*
- **Write-Ahead Log**:
  - сегментированные файлы `wal-NNNNNNNNNN.log` с rotation по размеру
  - три политики синхронизации: `always` / `everysec` / `no`
  - replay при старте через callback в storage
- **Лимит соединений** — семафор на каналах и отказ с `-ERR max number of clients reached`
- **Graceful shutdown** — слив in-flight соединений по таймауту, финальный fsync WAL, остановка фоновых горутин
- **Метрики** пока push-модель в VictoriaMetrics
- **Структурированные логи** через `zap`
- **Конфигурация** через YAML с валидацией и дефолтами
- **Docker** — multi-stage build на `scratch` (≈10 MB образ)

## В планах

- Команды: `EXPIRE`, `TTL`, `KEYS`, `DBSIZE`, `FLUSHDB`, `INFO`, `SET ... EX`
- CRC32 на запись WAL для обнаружения повреждений
- Snapshots + WAL-компакция после снимка
- Репликация через gRPC: начальный snapshot transfer + WAL stream
- Лимиты на размер ключа и значения

## Архитектура

```
┌──────────────────────────────────────────────┐
│                   Client                     │
│             (redis-cli, Go app)              │
└──────────────┬───────────────────────────────┘
               │ RESP over TCP
               ▼
┌──────────────────────────────────────────────┐
│              TCP Server (RESP)               │
│       accept loop, semaphore conn limit      │
└──────────────┬───────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────┐
│              Command Dispatcher              │
│              PING, GET, SET, DEL             │
└──────┬───────────────────┬───────────────────┘
       │                   │
       ▼                   ▼
┌─────────────┐    ┌──────────────┐
│  Storage    │    │     WAL      │
│ ShardedMap  │    │  Segmented   │
│  + TTL GC   │    │ + sync policy│
└─────────────┘    └──────────────┘
       │
       ▼
┌──────────────────────────────────────────────┐
│       Metrics (Prometheus push model)        │
│        VictoriaMetrics / Pushgateway         │
└──────────────────────────────────────────────┘
```

## Детали

### Storage
- Шардирование: `FNV-32(key) mod partitions_number` определяет шард, у каждого свой `RWMutex`
- `GET` берёт `RLock`; `SET`, `DEL` — `Lock`. Между шардами контеншна нет
- Запись в WAL идёт **до** мутации map
- TTL: при `GET` истёкший ключ удаляется лениво (`time.Now().After(*ExpiresAt)`). Параллельно тикер GC раз в `gc_interval` обходит **один шард за тик** по rotational cursor, до истечения `gc_budget`. Так GC не залипает на большом шарде и не блокирует рабочую нагрузку

### WAL
**Файлы и ротация.** Журнал хранится в файлах вроде wal-0000000001.log. Когда файл дорастает до заданного размера, создаётся новый, а старый принудительно сбрасывается на диск (fsync).

**Формат записей.** Каждая запись — это массив строк в формате RESP:

- ["SET", k, v] — сохранить значение
- ["SETX", k, v, expires_unix_nano] — сохранить с временем жизни
- ["DEL", k] — удалить

Когда данные реально попадают на диск (три режима):

- **always** — сброс на диск после каждой записи. Самый надёжный, но самый медленный.
- **everysec** — фоновый процесс раз в секунду проверяет, были ли новые записи, и если да — сбрасывает на диск. При смене файла или закрытии журнала всё тоже принудительно сбрасывается.
- **no** — данные просто копятся в буфере, на диск сбрасываются когда ОС сама решит. Самый быстрый, но при сбое можно потерять данные.

**Восстановление после перезапуска — replay.** При старте система читает журнал и заново применяет все записи. Если файл закончился ровно на границе записи — всё чисто. Если запись оборвалась на полуслове (например, питание пропало во время записи) — последняя неполная запись просто игнорируется с предупреждением в лог. Старые (уже закрытые) файлы считаются всегда целыми.

### Жизненный цикл (cmd/main.go)
Порядок старта: `Config → Logger → Metrics → WAL (+ replay) → Storage → Transport`.

Shutdown:
1. `signal.NotifyContext` ловит `SIGINT` / `SIGTERM`
2. Сервер крутится в горутине, закрывает `serverDone` при выходе из `Start`
3. Первый `select`: либо сигнал, либо неожиданный выход сервера
4. Второй `select` с `context.WithTimeout(shutdown_timeout)`: ждём слива in-flight соединений, либо force-close
5. `wal.Close()` — финальный flush + fsync + остановка everysec-горутины
6. В defer: `log.Sync()`, `signal.stop()`

### Метрики

Все метрики с префиксом `ararauna_`. Полный список с примерами PromQL — в [`docs/metrics.md`](docs/metrics.md).

| Группа | Метрики |
| --- | --- |
| Команды | `command_total{cmd}`, `command_duration_seconds{cmd}`, `command_errors_total{cmd}` |
| Storage | `get_hit_total`, `get_miss_total`, `get_expired_total`, `set_total`, `set_with_ttl_total`, `del_total` |
| WAL | `wal_append_total`, `wal_bytes_written_total`, `wal_append_duration_seconds`, `wal_fsync_total`, `wal_fsync_duration_seconds`, `wal_segment_total` |
| Соединения | `conn_accepted_total`, `conn_rejected_total`, `conn_active` (gauge через `atomic.Int64`) |
| GC | `gc_evicted_total` |

Также автоматически пушатся `go_*` и `process_*` runtime-метрики.

## Быстрый старт

### Локально

```bash
make build
./bin/ararauna --config config.yml

# или
make run
```

### Docker

```bash
docker build -t ararauna .
docker run -p 6379:6379 -v $(pwd)/data:/app/data ararauna
```

### Использование

```bash
$ redis-cli -p 6379

127.0.0.1:6379> PING
PONG
127.0.0.1:6379> SET hello world
OK
127.0.0.1:6379> GET hello
"world"
127.0.0.1:6379> DEL hello
(integer) 1
```

## Конфигурация

`config.yml`:

```yaml
server:
  port: 6379
  shutdown_timeout: 5s
  max_connections: 1024

storage:
  partitions_number: 16     # количество шардов
  gc_interval: 100ms        # как часто запускать GC TTL
  gc_budget: 50ms            # бюджет времени на один проход GC

logger:
  level: info
  file_path: ./ararauna.log

wal:
  enabled: true
  dir: ./data/wal
  segment_size: 16777216    # 16 MiB
  sync_policy: always       # always | everysec | no

metrics:
  enabled: true
  provider: victoriametrics # victoriametrics | prometheus
  push_url: "http://localhost:8428/api/v1/import/prometheus"
  job: ararauna
  push_interval: 10s
  extra_labels:
    instance: ararauna-1
```

## Поддерживаемые команды

| Команда | Описание |
| --- | --- |
| `PING` | Проверка живости — возвращает `PONG` |
| `SET key value` | Сохранить значение |
| `GET key` | Получить значение |
| `DEL key` | Удалить ключ |

Покрыто тестами: `parser`, `storage`, `wal`, `command`, `transport`, `metrics`, `concurrency`.

## Структура проекта

```
ararauna/
├── cmd/main.go              # entry point, lifecycle, graceful shutdown
├── internal/
│   ├── config/              # YAML loader + дефолты + валидация
│   ├── logger/              # инициализация zap
│   ├── toolbox/             # общий контейнер: { Cfg, Logger, Metrics }
│   ├── parser/              # RESP encode/decode
│   ├── errs/                # sentinel errors
│   ├── storage/             # шардированная map, TTL, GC, интеграция с WAL
│   ├── wal/                 # сегментированный WAL + replay
│   ├── command/             # RESP-диспетчер команд
│   ├── transport/           # TCP-сервер, accept loop, conn handlers
│   └── metrics/             # push-based recorder, nil-safe
├── pkg/
│   ├── concurrency/         # channel-based семафор
│   └── ptr/                 # ptr.Of[T](v) helper
├── docs/metrics.md          # описание метрик и примеры PromQL
├── data/wal/                # сегменты WAL (создаются на старте)
├── config.yml
├── Dockerfile
├── Makefile
└── go.mod
```

### Что используется из зависимостей:

- [`go.uber.org/zap`](https://github.com/uber-go/zap) — структурированные логи
- [`gopkg.in/yaml.v3`](https://gopkg.in/yaml.v3) — конфиг
- [`github.com/VictoriaMetrics/metrics`](https://github.com/VictoriaMetrics/metrics) — Prometheus exposition + push
- [`github.com/stretchr/testify`](https://github.com/stretchr/testify) — тесты

## Лицензия

MIT
