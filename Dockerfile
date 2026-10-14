# Образ для goreleaser (dockers_v2): бінарник уже зібрано, контекст містить
# його в каталозі $TARGETPLATFORM. Сервер працює через stdio:
#   docker run -i --rm -e PRRO_TOKEN ghcr.io/prro-cloud/prro-mcp
FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/prro-mcp /usr/local/bin/prro-mcp
LABEL io.modelcontextprotocol.server.name="io.github.prro-cloud/prro-mcp"
ENTRYPOINT ["/usr/local/bin/prro-mcp"]
