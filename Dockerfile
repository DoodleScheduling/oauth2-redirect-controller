FROM gcr.io/distroless/static:latest@sha256:87bce11be0af225e4ca761c40babb06d6d559f5767fbf7dc3c47f0f1a466b92c
WORKDIR /
COPY manager manager
USER 65532:65532

# User env is required by opentelemetry-go
ENV USER=webhook-controller

ENTRYPOINT ["/manager"]
