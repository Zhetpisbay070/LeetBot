FROM golang:1.24 as builder
COPY go.mod go.sum /go/src/github.com/oybek/jethouse/
WORKDIR /go/src/github.com/oybek/jethouse
RUN go mod download
COPY . /go/src/github.com/oybek/jethouse
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o build/jethouse github.com/oybek/jethouse

FROM alpine/curl
RUN apk add --no-cache ca-certificates && update-ca-certificates
COPY --from=builder /go/src/github.com/oybek/jethouse/build/jethouse /usr/bin/jethouse
EXPOSE 8080 8080
ENTRYPOINT ["/usr/bin/jethouse"]
