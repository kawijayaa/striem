FROM node:22-alpine AS web
WORKDIR /src
COPY package.json package-lock.json vite.config.js ./
COPY web/index.html ./web/index.html
COPY web/src ./web/src
RUN npm ci && npm run build

FROM golang:1.24-alpine AS build
WORKDIR /src
RUN apk add --no-cache gcc musl-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/striem ./cmd/striem

FROM alpine:3.22
RUN addgroup -S striem && adduser -S -G striem striem
WORKDIR /app
COPY --from=build /out/striem /usr/local/bin/striem
RUN mkdir /data && chown striem:striem /data
USER striem
ENV STRIEM_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["striem"]
