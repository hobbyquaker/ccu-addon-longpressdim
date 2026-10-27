#!/bin/sh
#
#   Builds the addon packages: dist/longpressdim-<arch>-<version>.tar.gz and .sha256 for aarch64,
#   x86_64 and armv7l (the asset names openccu-lite's catalogue resolves by uname -m).
#
#     VERSION=0.1.1 scripts/build.sh            # all three
#     ARCHES=x86_64 scripts/build.sh            # one
#     SKIP_UI=1 scripts/build.sh                # keep addon/longpressdim/app and www as they are
#
#   The web app is built with npm in ui/ and copied to addon/longpressdim/app (the daemon serves it at
#   /addons/longpressdim/); the settings page - the ccu-addon-kit frame with the Bindings tab - is built
#   the same way and vendored with the kit's CGIs into addon/longpressdim/www (/addons/longpressdim-config/),
#   together with addon/static (the 503 page); the manifest's icons are tracked in addon/longpressdim/www
#   itself, where the catalogue reads them from the repository at the release tag; the daemon is a
#   static Go binary (CGO_ENABLED=0) per architecture.
#
set -e
cd "$(dirname "$0")/.."
VERSION=${VERSION:-0.0.0-dev}
ARCHES=${ARCHES:-aarch64 x86_64 armv7l}
GO=${GO:-go}

if [ -z "$SKIP_UI" ]; then
    (cd ui && npm ci --no-audit --no-fund && npm run build)
    rm -rf addon/longpressdim/app
    mkdir -p addon/longpressdim/app
    cp -a ui/dist/. addon/longpressdim/app/
    # the kit: www/{settings.cgi,kit/,frame/}, lib/kit, bin/{kit-run,update_addon}, etc/lighttpd.conf
    ui/node_modules/.bin/ccu-addon-kit vendor --config addon/kit.json --out addon/longpressdim --frame ui/settings/dist
fi
cp -a addon/static/. addon/longpressdim/www/
printf 'VERSION_ADDON=%s\n' "$VERSION" > addon/longpressdim/versions

mkdir -p dist
for arch in $ARCHES; do
    case $arch in
        aarch64) goarch=arm64; goarm= ;;
        x86_64) goarch=amd64; goarm= ;;
        armv7l) goarch=arm; goarm=7 ;;
        *) echo "unknown architecture $arch" >&2; exit 1 ;;
    esac
    # bin/ also holds the kit's kit-run and update_addon: only the daemon is replaced
    mkdir -p addon/longpressdim/bin
    rm -f addon/longpressdim/bin/longpressdim
    CGO_ENABLED=0 GOOS=linux GOARCH=$goarch GOARM=$goarm $GO build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o addon/longpressdim/bin/longpressdim ./cmd/longpressdim

    out=dist/longpressdim-$arch-$VERSION.tar.gz
    stage=$(mktemp -d)
    cp -a addon/update_script addon/openccu-lite.json addon/longpressdim.cfg addon/longpressdim "$stage/"
    rm -rf "$stage/longpressdim/var" "$stage/longpressdim/etc/longpressdim.json"
    find "$stage" \( -name '._*' -o -name '.DS_Store' \) -exec rm -f {} +
    chmod 755 "$stage/update_script" "$stage/longpressdim/rc.d" "$stage/longpressdim/bin/longpressdim" "$stage/longpressdim/bin/kit-run" "$stage/longpressdim/bin/update_addon"
    find "$stage/longpressdim/www" -name '*.cgi' -exec chmod 755 {} +
    COPYFILE_DISABLE=1 tar --owner=root --group=root --sort=name --mtime='2026-01-01 00:00Z' -czf "$out" -C "$stage" .
    rm -rf "$stage"
    (cd dist && sha256sum "$(basename "$out")" > "$(basename "$out").sha256")
    echo "built $out"
done
