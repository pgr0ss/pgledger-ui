FROM golang:1.27.3 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pgledger-visualizer .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /pgledger-visualizer /pgledger-visualizer
ENV PGLEDGER_VISUALIZER_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/pgledger-visualizer"]
CMD ["serve"]
