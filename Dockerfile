FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/api ./cmd/api
RUN CGO_ENABLED=0 go build -o /bin/seed ./cmd/seed

FROM alpine:3.22
COPY --from=build /bin/api /bin/seed /usr/local/bin/
USER nobody
CMD ["api"]
