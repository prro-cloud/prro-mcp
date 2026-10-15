# Образ для goreleaser (dockers_v2): бінарник уже зібрано, контекст містить
# його в каталозі $TARGETPLATFORM. Локальний сервер працює через stdio,
# віддалений — через HTTP із токеном від клієнта:
#   docker run -i --rm -e PRRO_TOKEN ghcr.io/prro-cloud/prro-mcp
#   docker run -d -p 127.0.0.1:8080:8080 ghcr.io/prro-cloud/prro-mcp -http :8080
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/prro-mcp /usr/local/bin/prro-mcp
LABEL io.modelcontextprotocol.server.name="io.github.prro-cloud/prro-mcp"
ENTRYPOINT ["/usr/local/bin/prro-mcp"]
