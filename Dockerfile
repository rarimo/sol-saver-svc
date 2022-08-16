FROM golang:1.17-alpine as buildbase

WORKDIR /go/src/gitlab.com/gitlab.com/rarify-protocol/sol-saver-svc
COPY vendor .
COPY . .

ENV GO111MODULE="on"
ENV CGO_ENABLED=0
ENV GOOS="linux"

RUN go build -o /usr/local/bin/sol-saver-svc gitlab.com/gitlab.com/rarify-protocol/sol-saver-svc


###

FROM alpine:3.9

COPY --from=buildbase /usr/local/bin/sol-saver-svc /usr/local/bin/sol-saver-svc
RUN apk add --no-cache ca-certificates

ENTRYPOINT ["sol-saver-svc"]
