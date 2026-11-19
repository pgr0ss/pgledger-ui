FROM golang:1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pgledger-ui .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /pgledger-ui /pgledger-ui
ENV PGLEDGER_UI_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/pgledger-ui"]
CMD ["serve"]
