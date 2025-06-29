FROM gcr.io/distroless/static-debian12
ENTRYPOINT ["/maybe-dont"]
COPY maybe-dont /
