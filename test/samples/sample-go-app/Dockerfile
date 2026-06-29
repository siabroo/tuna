FROM golang:1.26 AS build
WORKDIR /src
COPY main.go go.mod go.sum ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /sample-go-app .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /sample-go-app /sample-go-app
USER 65532:65532
ENTRYPOINT ["/sample-go-app"]
