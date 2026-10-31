# Зміни

Формат — [Keep a Changelog](https://keepachangelog.com/uk/1.1.0/), версії — за
[семантичним версіонуванням](https://semver.org/lang/uk/). До 1.0.0 мінорна
версія може містити несумісні зміни.

## [Unreleased]

### Додано

- Віддалений сервер: прапорець `-http :8080` вмикає Streamable HTTP без
  сесій на `/mcp` і перевірку живості на `/healthz`. Токен кожен клієнт
  передає в `Authorization: Bearer`; сервер пересилає його в API і не
  зберігає. Ліміт запитів на токен — `PRRO_MCP_RATE_LIMIT` (типово 120 на
  хвилину).
- Прапорець `-healthcheck URL` для HEALTHCHECK Docker: в образі distroless
  немає ні shell, ні wget.
- `deploy/compose.yaml` за специфікацією Compose і інструкція розгортання
  з прикладами Caddy і nginx.
- `server.json`: віддалений сервер `https://mcp.prro.cloud/mcp` у `remotes`.

- `get_task`: у текстовому зведенні завдання закриття зміни — підсумки
  Z-звіту, подані до ДПС (`result.zreport.totals`).
- `get_cash_register_state`: зведення повідомляє, що надсилання
  офлайн-пакетів до ДПС зупинено (`offline_session.submit_paused_at`), з
  помилкою ДПС (`submit_fault`), а також що офлайн-межі знято
  (`offline_limits.enforced=false`) або повернуто (`restored_at`). Опис
  інструмента пояснює ці поля.

### Змінено

- prro-go 0.4.0, контракт API 0.10.0: ресурс `prro://openapi.yaml`
  оновлено (make sync-spec).
- prro-go 0.3.0, контракт API 0.8.0: ресурс `prro://openapi.yaml`
  оновлено; опис `get_task` згадує простір кодів збою `storno.*` і
  підсумки Z-звіту з розподілом за податками.
- Підказка до помилки 401 пояснює, де замінити токен і для локального, і
  для віддаленого сервера.

## [0.1.0] — 2026-10-14

Перший випуск: prro-go 0.2.0, контракт API 0.6.0.

### Додано

- MCP-сервер для API PRRO.cloud через stdio; конфігурація зі змінних
  оточення `PRRO_TOKEN`, `PRRO_BASE_URL`, `PRRO_LANG`, `PRRO_MCP_LOG_LEVEL`.
- Десять інструментів лише для читання: `get_version`, `whoami`,
  `get_dps_status`, `list_cash_registers`, `get_cash_register_state`,
  `get_task`, `get_billing_usage`, `list_invoices`, `list_webhooks`,
  `list_webhook_deliveries`. Результат — структуровані дані за вихідною
  схемою і текстове зведення.
- Ресурс `prro://openapi.yaml` — публічний контракт API.
- Помилки API повертаються як результат інструмента з `isError` і
  підказкою, що робити.
- Журнал викликів інструментів у stderr без токена й аргументів.
- Тест контракту: кожна операція OpenAPI або обслуговується інструментом,
  або явно виключена.
- Збірки для Linux, macOS і Windows, пакет `prro-mcp.mcpb` для Claude
  Desktop, образ `ghcr.io/prro-cloud/prro-mcp`.

[Unreleased]: https://github.com/prro-cloud/prro-mcp/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/prro-cloud/prro-mcp/releases/tag/v0.1.0
