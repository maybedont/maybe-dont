FROM debian:12-slim
RUN apt-get update && apt-get -y install ca-certificates && apt-get clean && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["/maybe-dont"]
COPY maybe-dont /
