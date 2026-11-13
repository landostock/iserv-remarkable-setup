ARG REMARQUEE_COMMIT=08ab8e048a30cac72f9d7560b0c54aaee87c9f4e
ARG RMAPI_FORK_COMMIT=f295d5466978954edabaae4c730d50d03740cfac

FROM golang:1.26.11-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0 AS remarquee-builder
ARG REMARQUEE_COMMIT
ARG RMAPI_FORK_COMMIT
COPY rmapi_safe_api.go.txt /tmp/rmapi_safe_api.go
RUN git clone https://github.com/go-go-golems/remarquee.git /src/remarquee \
    && cd /src/remarquee \
    && git checkout ${REMARQUEE_COMMIT} \
    && git clone https://github.com/FNStudios-NI/rmapi.git /src/rmapi \
    && cd /src/rmapi \
    && git checkout ${RMAPI_FORK_COMMIT} \
    && sed -i 's#https://my.remarkable.com/token/json/2/#https://webapp-prod.cloud.remarkable.engineering/token/json/2/#g' auth/auth.go \
    && cp /tmp/rmapi_safe_api.go api/api.go \
    && go mod edit -droprequire=github.com/golang-jwt/jwt
RUN cd /src/remarquee \
    && go mod edit -replace github.com/juruen/rmapi=/src/rmapi \
    && go get google.golang.org/grpc@v1.83.1 golang.org/x/net@v0.60.0 \
    && go mod tidy \
    && go build -o /out/remarquee ./cmd/remarquee

FROM python:3.13-slim-bookworm@sha256:a5a7017efc5969e00ae2b91c8eb0631049a569652aa4ffc4501769049ae45c72
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates rclone \
    && rm -rf /var/lib/apt/lists/*
COPY --from=remarquee-builder /out/remarquee /usr/local/bin/remarquee
WORKDIR /app
COPY sync_job.py config.json ./
ENV DATA_DIR=/data \
    RMAPI_CONFIG=/data/rmapi.conf \
    SYNC_CONFIG=/app/config.json \
    PYTHONUNBUFFERED=1 \
    DRY_RUN=true \
    MIN_RUNTIME_SECONDS=12
CMD ["python3", "/app/sync_job.py"]
