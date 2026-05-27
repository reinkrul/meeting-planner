FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/meeting-planner ./cmd/meeting-planner

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/meeting-planner /meeting-planner
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/meeting-planner"]
