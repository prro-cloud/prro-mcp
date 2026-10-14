# Зміни

Формат — [Keep a Changelog](https://keepachangelog.com/uk/1.1.0/), версії — за
[семантичним версіонуванням](https://semver.org/lang/uk/). До 1.0.0 мінорна
версія може містити несумісні зміни.

## [Unreleased]

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

[Unreleased]: https://github.com/prro-cloud/prro-mcp/commits/main
