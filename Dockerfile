FROM golang:1.11.4-alpine3.8 as builder

RUN mkdir /app
WORKDIR /app
RUN apk add --update --no-cache ca-certificates git
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o /go/bin/go-test-rp

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/bin/go-test-rp /go/bin/go-test-rp
ENTRYPOINT ["/go/bin/go-test-rp"]