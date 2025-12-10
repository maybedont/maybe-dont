ARG TARGETPLATFORM
FROM --platform=$TARGETPLATFORM debian:12-slim

# Update image and install ca-certificates
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

# Create a non-root group and user
RUN mkdir -p /usr/local/maybedont \
  && groupadd -g 999 maybedont \
  && useradd -u 999 -d /usr/local/maybedont -m -g maybedont maybedont \
  && chown maybedont:maybedont /usr/local/maybedont

COPY --chown=maybedont:maybedont maybe-dont /usr/local/maybedont/
USER maybedont
ENTRYPOINT ["/usr/local/maybedont/maybe-dont"]