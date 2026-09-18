FROM node:24-bookworm-slim@sha256:2fe369e969550cde8e867afc3fe370b260140cab4a23d467074295b42163d553 AS fixture-build
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc pkg-config libgtk-3-dev xvfb dbus-x11 at-spi2-core \
    && rm -rf /var/lib/apt/lists/*
COPY fixture/main.c /tmp/fixture.c
RUN gcc /tmp/fixture.c -o /tmp/cherry-native-fixture $(pkg-config --cflags --libs gtk+-3.0)

FROM node:24-bookworm-slim@sha256:2fe369e969550cde8e867afc3fe370b260140cab4a23d467074295b42163d553
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgtk-3-0 libglib2.0-bin xvfb dbus-x11 at-spi2-core \
    && rm -rf /var/lib/apt/lists/* \
    && ! command -v python3
COPY --from=fixture-build /tmp/cherry-native-fixture /usr/local/bin/cherry-native-fixture
CMD ["bash", "experiments/LinuxNativeProbe/run.sh"]
