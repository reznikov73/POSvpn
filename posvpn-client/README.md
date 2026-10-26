# posvpn-client

Клиент POSvpn. Сейчас — CLI: создание сети, вход, просмотр участников.

```powershell
go run . create -server http://127.0.0.1:8787 -network office -password secret123 -node PC-1
go run . join   -server http://127.0.0.1:8787 -network office -password secret123 -node PC-2
go run . status
```

Настройки и приватный ключ узла лежат в `%AppData%\POSvpn\client.json`
с правами `0600`. Приватный ключ не покидает машину: на сервер уходит только
публичная половина.

Команды:

| Команда | Что делает |
|---|---|
| `create` | создаёт сеть, узел становится администратором |
| `join` | входит в существующую сеть по имени и паролю |
| `status` / `peers` | печатает карту сети |

Дальше по плану: служба Windows с Wintun и туннелем (этап 1–2), затем GUI на
Wails с треем (этап 3).
