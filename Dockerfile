FROM harbor.tuxgrid.com/docker.io/golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod .
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /attest-coordinator .

FROM scratch
COPY --from=build /attest-coordinator /attest-coordinator
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENTRYPOINT ["/attest-coordinator"]
