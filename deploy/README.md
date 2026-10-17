# Розгортання віддаленого сервера

`prro-mcp -http :8080` — віддалений MCP-сервер: Streamable HTTP без сесій на
`/mcp`, перевірка живості на `/healthz`. Клієнт передає машинний токен
PRRO.cloud у заголовку `Authorization: Bearer`; сервер пересилає його в API і
ніде не зберігає. Власних секретів у сервера немає, стану теж — будь-яку
версію можна відкотити на будь-яку іншу.

Файли:

```
compose.yaml   сервіс за специфікацією Compose, без розширень платформ
.env.example   змінні для нього
```

## Запуск

**docker compose / podman compose:**

```sh
cp .env.example .env        # PRRO_MCP_VERSION, PRRO_MCP_PUBLISH
docker compose up -d
curl -fsS http://127.0.0.1:8080/healthz      # ok
```

**Панелі керування**, що розгортають стек із compose-файлу (Portainer,
Coolify, Dokploy тощо): вставте `compose.yaml` як є і задайте змінні з
`.env.example` у змінних стека. Файл не використовує `env_file` — усе, що
бачить контейнер, перелічено в `environment:`, тож він працює й там, де
змінні стека доступні лише для підстановки `${…}`.

**Docker Swarm** (`docker stack deploy`): `restart` і `stop_grace_period`
Swarm бере з `deploy:`, а адреса в `ports` не підтримується — публікуйте
порт через ingress або оверлейну мережу проксі.

**Kubernetes:** окремого маніфесту немає. Усе потрібне — у `compose.yaml`:
образ, аргументи `-http :8080`, змінні оточення, `httpGet` на `/healthz` для
проб, `terminationGracePeriodSeconds: 30`, `readOnlyRootFilesystem` і
`runAsNonRoot` (образ уже працює від `nonroot`).

### Змінні

| змінна | за замовчуванням | що це |
|---|---|---|
| `PRRO_MCP_VERSION` | — (обов'язкова) | версія образу без `v`: `0.2.0` |
| `PRRO_MCP_PUBLISH` | `127.0.0.1:8080` | адреса на хості для проксі |
| `PRRO_MCP_IMAGE` | `ghcr.io/prro-cloud/prro-mcp` | образ |
| `PRRO_BASE_URL` | `https://api.prro.cloud` | адреса API |
| `PRRO_LANG` | `uk` | мова помилок API: `uk` або `en` |
| `PRRO_MCP_LOG_LEVEL` | `info` | `debug` показує запити до API (без токенів) |
| `PRRO_MCP_RATE_LIMIT` | `120` | запитів на хвилину з одним токеном; `0` — без ліміту |
| `PRRO_MCP_MEMORY` | `128M` | ліміт пам'яті контейнера |

`PRRO_TOKEN` не задавайте: з `-http` сервер із ним не стартує.

## Зворотний проксі

TLS, ліміт за IP і таймаути — на проксі перед сервером. Що йому потрібно
знати:

- відповіді — звичайний JSON, не SSE: буферизацію вимикати не треба;
- виклик інструмента — це запит до API, prro-go чекає до 60 секунд і
  повторює безпечні запити; сервер сам обриває відповідь через 5 хвилин —
  таймаут читання проксі має бути не меншим;
- `/healthz` потрібен лише оркестратору, назовні його можна закрити;
- ліміт сервера рахується за токеном, а чи справжній токен, знає лише API:
  потік запитів з вигаданими токенами зупиняє ліміт за IP на проксі.
  Підключення клієнта — кілька запитів поспіль (`initialize`,
  `tools/list`), тож потрібен `burst`.

**Caddy:**

```
mcp.example.com {
	respond /healthz 404
	reverse_proxy 127.0.0.1:8080 {
		transport http {
			read_timeout 300s
		}
	}
}
```

Ліміт за IP у Caddy — сторонній модуль
[caddy-ratelimit](https://github.com/mholt/caddy-ratelimit).

**nginx:**

```nginx
# у контексті http
limit_req_zone $binary_remote_addr zone=prro_mcp:10m rate=10r/s;

server {
    server_name mcp.example.com;
    # listen 443 ssl; сертифікати — як зазвичай

    location = /healthz { return 404; }

    location / {
        limit_req zone=prro_mcp burst=40 nodelay;
        limit_req_status 429;
        proxy_pass http://127.0.0.1:8080;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
```

## Перевірка

```sh
curl -sS -o /dev/null -w '%{http_code}\n' -X POST https://mcp.example.com/mcp   # 401 — без токена

# Повний шлях до API: get_version працює з будь-яким токеном.
curl -sS https://mcp.example.com/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H 'MCP-Protocol-Version: 2025-06-18' -H 'Authorization: Bearer test' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_version","arguments":{}}}'
```

З клієнта — з тестовим токеном:

```sh
claude mcp add --transport http prro https://mcp.example.com/mcp \
  --header "Authorization: Bearer <токен>"
```

## Безпека

- **Публікуйте порт лише на loopback або приватній адресі.** Опублікований
  порт Docker обходить файрвол хоста (ufw, firewalld), а `0.0.0.0` віддав би
  сервер в інтернет по відкритому HTTP повз TLS.
- **Не підключайте сервер до внутрішніх мереж.** Він — звичайний клієнт
  публічного API і доступний усьому інтернету; якщо його колись
  скомпрометують, він має опинитися в порожній мережі, а не поруч із базою.
  Навіть коли API працює на тому ж хості, ходіть у нього за публічною
  адресою.
- **Секретів у конфігурації немає** — токени приходять від клієнтів і в
  журнал не потрапляють (лише відбиток SHA-256 при перевищенні ліміту).

## Оновлення

Змініть `PRRO_MCP_VERSION` і перезапустіть: `docker compose pull && docker
compose up -d` (у панелях — оновити стек із повторним завантаженням образу).
Docker дає поточним викликам до 30 секунд, клієнти MCP перепідключаються самі.
