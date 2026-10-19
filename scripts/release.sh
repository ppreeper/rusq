#!/usr/bin/env bash
#
# release.sh builds the cross-platform release archives into dist/.
#
# It is the engine behind `make release` and the only build step in
# .github/workflows/release.yml, so cutting a release is a single tag push.
#
# Every binary is stamped with the version, commit and build date, which means
# `rusq version` on a downloaded archive reports exactly what you are running.
#
# Usage: scripts/release.sh [VERSION]
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
VERSION_PKG=github.com/ppreeper/rusq/internal/version
LDFLAGS="-s -w -X ${VERSION_PKG}.Version=${VERSION} -X ${VERSION_PKG}.Commit=${COMMIT} -X ${VERSION_PKG}.Date=${DATE}"

TARGETS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

rm -rf dist
mkdir -p dist

echo "building ${VERSION} from ${COMMIT} at ${DATE}"

for target in ${TARGETS}; do
	os="${target%/*}"
	arch="${target#*/}"
	name="rusq-${VERSION}-${os}-${arch}"
	stage="dist/${name}"

	mkdir -p "${stage}"
	if [ "${os}" = "windows" ]; then
		binary="rusq.exe"
	else
		binary="rusq"
	fi

	CGO_ENABLED=0 GOOS="${os}" GOARCH="${arch}" \
		go build -trimpath -ldflags "${LDFLAGS}" -o "${stage}/${binary}" ./cmd/rusq
	cp LICENSE "${stage}/"

	# Windows users expect a .zip; everything else is a tarball.
	if [ "${os}" = "windows" ] && command -v zip >/dev/null 2>&1; then
		(cd dist && zip -qr "${name}.zip" "${name}")
	else
		tar -czf "dist/${name}.tar.gz" -C dist "${name}"
	fi

	rm -rf "${stage}"
	echo "  ${name}"
done

# Checksums, with a macOS fallback for machines without coreutils.
(
	cd dist
	: > checksums.txt
	for f in *.tar.gz *.zip; do
		[ -e "${f}" ] || continue
		if command -v sha256sum >/dev/null 2>&1; then
			sha256sum "${f}"
		else
			shasum -a 256 "${f}"
		fi
	done > checksums.txt
)

# Prove the stamp took: extract the linux/amd64 archive and run it.
archive="dist/rusq-${VERSION}-linux-amd64.tar.gz"
if [ -f "${archive}" ]; then
	tmp="$(mktemp -d)"
	tar -xzf "${archive}" -C "${tmp}"
	"${tmp}/rusq-${VERSION}-linux-amd64/rusq" version
	rm -rf "${tmp}"
fi

ls -l dist
