FROM golang:1.27 AS build

ARG CMD=gate

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/${CMD}

FROM gcr.io/distroless/static-debian12

COPY --from=build /out/app /app

ENTRYPOINT ["/app"]
