# Builds one image per program. Pick the program with the CMD_PATH build arg:
#   docker build --build-arg CMD_PATH=./services/booking/cmd/booking-api .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG CMD_PATH
RUN test -n "$CMD_PATH" && CGO_ENABLED=0 go build -o /bin/app "$CMD_PATH"

FROM alpine:3.22
COPY --from=build /bin/app /usr/local/bin/app
USER nobody
ENTRYPOINT ["app"]
