FROM node:22-alpine AS web
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY tsconfig.json vite.config.js ./
COPY web/index.html web/kusto.html ./web/
COPY web/src ./web/src
COPY web/public ./web/public
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
RUN apk add --no-cache gcc musl-dev
COPY go.mod go.sum ./
# Supply the independent compiler checkout with --build-context ksql=../ksql.
COPY --from=ksql / /ksql
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=1 go build -tags sqlite_fts5 -trimpath -ldflags="-s -w" -o /out/striem ./cmd/striem

FROM alpine:3.24
RUN addgroup -S striem && adduser -S -G striem striem
WORKDIR /app
COPY --from=build /out/striem /usr/local/bin/striem
RUN mkdir /data && chown striem:striem /data
USER striem
ENV STRIEM_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["striem"]
