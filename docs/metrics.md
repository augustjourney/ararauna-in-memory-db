# Метрики

Все метрики пишутся в Prometheus exposition format и отправляются push-ом на `metrics.push_url` каждые `metrics.push_interval`.

Префикс `ararauna_` — у всех метрик.  
Процессные метрики (`go_*`, `process_*`) добавляются автоматически через `vmm.WriteProcessMetrics` / `vmm.WriteFDMetrics`.

---

## Команды

| Метрика | Тип | Лейбл | Что измеряет |
|---|---|---|---|
| `ararauna_command_total` | Counter | `cmd` | Общее число выполненных команд |
| `ararauna_command_duration_seconds` | Histogram | `cmd` | Латентность команды от парсинга до ответа |
| `ararauna_command_errors_total` | Counter | `cmd` | Команды, вернувшие ответ с ошибкой (тип `-ERR`) |

Лейбл `cmd` принимает значения: `ping`, `get`, `set`, `del`, `other`.  
Неизвестные команды попадают в bucket `other`.

> `command_duration_seconds` включает время до WAL-fsync у `SET`/`DEL` при политике `always`.

---

## GET

| Метрика | Тип | Что измеряет |
|---|---|---|
| `ararauna_get_hit_total` | Counter | GET нашёл живой ключ |
| `ararauna_get_miss_total` | Counter | GET не нашёл ключ |
| `ararauna_get_expired_total` | Counter | GET нашёл ключ, но он истёк (lazy expiry) |

Сумма `hit + miss + expired` равна числу GET-запросов без ошибок парсинга/контекста.

---

## Запись

| Метрика | Тип | Что измеряет |
|---|---|---|
| `ararauna_set_total` | Counter | Успешных SET (включая перезапись) |
| `ararauna_set_with_ttl_total` | Counter | SET с TTL (подмножество `set_total`) |
| `ararauna_del_total` | Counter | Успешных DEL (только когда ключ существовал) |

---

## WAL

| Метрика | Тип | Что измеряет |
|---|---|---|
| `ararauna_wal_append_total` | Counter | Записей добавлено в WAL |
| `ararauna_wal_bytes_written_total` | Counter | Байт записано в WAL (encoded RESP payload) |
| `ararauna_wal_append_duration_seconds` | Histogram | Полная латентность `Append` (encode + write + fsync если `always` + rotate если нужно) |
| `ararauna_wal_fsync_total` | Counter | Вызовов `fsync`: при политике `always` — каждый append; при `everysec` — раз в секунду когда буфер грязный |
| `ararauna_wal_fsync_duration_seconds` | Histogram | Длительность каждого `fsync` |
| `ararauna_wal_segment_total` | Counter | Ротаций сегментов (файл перевалил за `segment_size`) |

---

## Сеть

| Метрика | Тип | Что измеряет |
|---|---|---|
| `ararauna_conn_accepted_total` | Counter | TCP-соединений принято с момента старта |
| `ararauna_conn_active` | Gauge | Активных соединений прямо сейчас |

---

## GC

| Метрика | Тип | Что измеряет |
|---|---|---|
| `ararauna_gc_evicted_total` | Counter | Ключей удалено фоновым GC-свипером (истёкший TTL) |

Не включает ключи, удалённые через lazy expiry в `Get` — те попадают в `get_expired_total`.

---

## Процессные метрики (runtime)

Добавляются автоматически в каждый push.

| Префикс | Источник | Примеры |
|---|---|---|
| `go_*` | `vmm.WriteProcessMetrics` | `go_goroutines`, `go_threads`, `go_heap_alloc_bytes` |
| `process_*` | `vmm.WriteFDMetrics` | `process_open_fds`, `process_max_fds` |

---

## Примеры запросов (VictoriaMetrics / PromQL)

```promql
# Hit rate для GET
rate(ararauna_get_hit_total[1m])
  / (rate(ararauna_get_hit_total[1m]) + rate(ararauna_get_miss_total[1m]))

# P99 латентность команд
histogram_quantile(0.99, rate(ararauna_command_duration_seconds_bucket[5m]))

# Скорость записи в WAL (байт/с)
rate(ararauna_wal_bytes_written_total[1m])

# Среднее время fsync
rate(ararauna_wal_fsync_duration_seconds_sum[1m])
  / rate(ararauna_wal_fsync_total[1m])

# Утечка соединений (растёт conn_active, но conn_accepted не растёт)
ararauna_conn_active
```
