FROM golang:1.18-alpine
WORKDIR /app
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY . ./
ENV GIN_MODE release
RUN go build -o /am-gitlab
EXPOSE 8080
CMD [ "/am-gitlab" ]
