FROM alpine:3.21

ARG UID=1000
ARG GID=1000

# Upgrade base packages and install ca-certificates
RUN apk upgrade --no-cache \
  && apk add --no-cache ca-certificates

# Create a non-root group and user
RUN addgroup -S -g "$GID" maybedont \
  && adduser -S -u "$UID" -G maybedont -h /usr/local/maybedont maybedont \
  && chown maybedont:maybedont /usr/local/maybedont \
  && chmod 700 /usr/local/maybedont

COPY --chown=maybedont:maybedont maybe-dont /usr/local/maybedont/

EXPOSE 8080
USER maybedont

ENTRYPOINT ["/usr/local/maybedont/maybe-dont"]