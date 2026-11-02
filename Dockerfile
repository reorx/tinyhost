# Release archives are built once by make dist and reused in the image.
FROM --platform=$BUILDPLATFORM busybox:1.37.0 AS unpack
ARG VERSION
ARG TARGETARCH
COPY dist/tinyhost-server_${VERSION}_linux_${TARGETARCH}.tar.gz /tmp/server.tar.gz
RUN tar -xzf /tmp/server.tar.gz -C /tmp tinyhost-server

FROM gcr.io/distroless/static:latest
COPY --from=unpack /tmp/tinyhost-server /tinyhost-server
VOLUME /data
EXPOSE 3471
ENTRYPOINT ["/tinyhost-server"]
CMD ["serve", "--data-dir", "/data", "--listen", ":3471"]
