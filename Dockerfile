FROM golang:1.25.1 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p web \
 && GOOS=js GOARCH=wasm go build -o web/collab.wasm ./wasm \
 && cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /collabd ./cmd/collabd

FROM scratch
COPY --from=build /collabd /collabd
VOLUME /data
EXPOSE 8080
ENV GOMEMLIMIT=900MiB
ENV COLLAB_DB=/data/collab.db
ENTRYPOINT ["/collabd"]
