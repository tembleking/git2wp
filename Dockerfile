FROM golang:alpine as builder
WORKDIR /go/src/git2wp/
RUN apk add --no-cache git
COPY main.go .
RUN go get -v
RUN CGO_ENABLED=0 go build

FROM alpine:edge
RUN apk --no-cache add ca-certificates
COPY --from=builder /go/src/git2wp/git2wp /git2wp
ENTRYPOINT ["/git2wp"]