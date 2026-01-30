FROM alpine:3.21

ARG UID=1000
ARG GID=1000

# Upgrade base packages and install ca-certificates
RUN apk upgrade --no-cache \
  && apk add --no-cache ca-certificates

# Create a non-root group and user with conventional home directory
# This enables XDG Base Directory defaults:
#   Config: /home/maybedont/.config/maybe-dont
#   State:  /home/maybedont/.local/state/maybe-dont
RUN addgroup -S -g "$GID" maybedont \
  && adduser -S -u "$UID" -G maybedont -h /home/maybedont maybedont

COPY --chown=maybedont:maybedont maybe-dont /home/maybedont/

EXPOSE 8080
USER maybedont
WORKDIR /home/maybedont

ENTRYPOINT ["/home/maybedont/maybe-dont"]
CMD ["start"]