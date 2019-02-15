FROM golang as builder
# Copy the local package files to the container's workspace.
COPY . /go/src/github.com/knative/build-pipeline/
# RUN go install github.com/knative/build-pipeline/test/gohelloworld
WORKDIR /go/src/github.com/knative/build-pipeline/test/gohelloworld
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app .

FROM alpine
WORKDIR /helloworld
COPY --from=builder /go/src/github.com/knative/build-pipeline/test/gohelloworld/app .
ENTRYPOINT /helloworld/app

# Document that the service listens on port 8080.
EXPOSE 8080