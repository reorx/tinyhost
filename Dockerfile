# Release archives are built once by make dist and reused in the image.
FROM --platform=$BUILDPLATFORM busybox:1.37.0 AS unpack
ARG VERSION
ARG TARGETARCH
COPY dist/tinyhost-server_${VERSION}_linux_${TARGETARCH}.tar.gz /tmp/server.tar.gz
RUN tar -xzf /tmp/server.tar.gz -C /tmp tinyhost-server
RUN mkdir -m 0700 /tmp/data && chown 65532:65532 /tmp/data

FROM gcr.io/distroless/static:latest
COPY --from=unpack /tmp/tinyhost-server /tinyhost-server
# A fresh named volume starts as a copy of /data, owner and mode included, so
# the nonroot user can write to it. Bind mounts keep the host directory's owner.
COPY --from=unpack --chown=65532:65532 /tmp/data /data
# distroless nonroot; deployments that pass user 65532 explicitly are unaffected.
USER 65532:65532
VOLUME /data
EXPOSE 3471
# distroless has no shell or curl, so the server binary probes itself. --listen
# matches CMD; containers that serve elsewhere must override both.
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 CMD ["/tinyhost-server", "healthcheck", "--listen", ":3471"]
ENTRYPOINT ["/tinyhost-server"]
CMD ["serve", "--data-dir", "/data", "--listen", ":3471"]
