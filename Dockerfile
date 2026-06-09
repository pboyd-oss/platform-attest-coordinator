FROM harbor.tuxgrid.com/docker.io/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /attest-coordinator . && \
    go clean -cache && \
    rm -rf /go/pkg/

FROM scratch
COPY --from=build /attest-coordinator /attest-coordinator
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/attest-coordinator"]
