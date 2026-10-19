#!/usr/bin/env bash
#
# Package rusq-sql once per platform, bundling the matching rusq binary so the
# extension works with no separate install. With VSCE_PAT and/or OVSX_PAT set
# it also publishes each VSIX to the VS Code Marketplace and Open VSX.
#
# Usage:
#   bash scripts/publish.sh                        # package, then publish both registries if tokens are set
#   bash scripts/publish.sh --package-only         # package only (npm run package:targets)
#   bash scripts/publish.sh --registry ovsx        # package, then publish to Open VSX only
#   bash scripts/publish.sh --registry marketplace # package, then publish to the VS Code Marketplace only
#
# Republishing is safe: each registry rejects an already-published version
# rather than replacing it, and the script treats that as success so it can be
# re-run after a partial publish.
#
set -euo pipefail

EXT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$EXT_DIR/../.." && pwd)"

PACKAGE_ONLY=0
REGISTRY="both"
while [ "$#" -gt 0 ]; do
	case "$1" in
	--package-only)
		PACKAGE_ONLY=1
		;;
	--registry)
		REGISTRY="${2:?--registry needs a value}"
		shift
		;;
	--registry=*)
		REGISTRY="${1#*=}"
		;;
	*)
		echo "unknown argument: $1" >&2
		exit 2
		;;
	esac
	shift
done

case "$REGISTRY" in
both | marketplace | ovsx) ;;
*)
	echo "unknown registry: $REGISTRY (want both, marketplace or ovsx)" >&2
	exit 2
	;;
esac

want_marketplace=0
want_ovsx=0
case "$REGISTRY" in
marketplace) want_marketplace=1 ;;
ovsx) want_ovsx=1 ;;
both)
	want_marketplace=1
	want_ovsx=1
	;;
esac

cd "$EXT_DIR"

PUBLISHER="$(node -p "require('./package.json').publisher")"
VERSION="$(node -p "require('./package.json').version")"
COMMIT="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X github.com/ppreeper/rusq/internal/version.Version=$VERSION -X github.com/ppreeper/rusq/internal/version.Commit=$COMMIT -X github.com/ppreeper/rusq/internal/version.Date=$DATE"

TARGETS=(linux-x64 linux-arm64 darwin-x64 darwin-arm64 win32-x64 win32-arm64)

goos_goarch() {
	case "$1" in
	linux-x64) echo "linux amd64" ;;
	linux-arm64) echo "linux arm64" ;;
	darwin-x64) echo "darwin amd64" ;;
	darwin-arm64) echo "darwin arm64" ;;
	win32-x64) echo "windows amd64" ;;
	win32-arm64) echo "windows arm64" ;;
	*)
		echo "unknown target $1" >&2
		return 1
		;;
	esac
}

echo "rusq-sql $VERSION ($COMMIT) publisher=$PUBLISHER registry=$REGISTRY"

# vsce runs the vscode:prepublish script (npm run build) itself, so dist/ is
# always rebuilt from the current source.

VSIXES=()

for target in "${TARGETS[@]}"; do
	read -r goos goarch <<<"$(goos_goarch "$target")"
	bin="rusq"
	if [ "$goos" = "windows" ]; then
		bin="rusq.exe"
	fi

	echo "==> $target (GOOS=$goos GOARCH=$goarch)"
	(
		cd "$REPO_ROOT"
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
			go build -trimpath -ldflags "$LDFLAGS" -o "$EXT_DIR/$bin" ./cmd/rusq
	)
	if [ "$goos" != "windows" ]; then
		chmod +x "$EXT_DIR/$bin"
	fi

	vsix="$EXT_DIR/rusq-sql-$VERSION@$target.vsix"
	npx --no-install vsce package --target "$target" --out "$vsix"

	# The binary is never committed; it only lives long enough to be packed.
	rm -f "$EXT_DIR/$bin"
	VSIXES+=("$vsix")
done

# A package without --target is the fallback VS Code uses on platforms that
# have no platform-specific package; it looks for rusq on PATH instead.
echo "==> generic (binary resolved from PATH)"
generic="$EXT_DIR/rusq-sql-$VERSION.vsix"
npx --no-install vsce package --out "$generic"
VSIXES+=("$generic")

echo
echo "Packaged:"
printf '  %s\n' "${VSIXES[@]}"

if [ "$PACKAGE_ONLY" = 1 ]; then
	exit 0
fi

# A registry whose token is absent is skipped, not fatal: `both` with one token
# still publishes that one.
if [ "$want_marketplace" = 1 ] && [ -z "${VSCE_PAT:-}" ]; then
	echo "note: VSCE_PAT is not set; skipping the VS Code Marketplace" >&2
	want_marketplace=0
fi
if [ "$want_ovsx" = 1 ] && [ -z "${OVSX_PAT:-}" ]; then
	echo "note: OVSX_PAT is not set; skipping Open VSX" >&2
	want_ovsx=0
fi

if [ "$want_marketplace" = 0 ] && [ "$want_ovsx" = 0 ]; then
	cat <<EOF

No publishing token is set for the requested registry ($REGISTRY), so nothing was published.

Publish by hand with:
  npx vsce publish --packagePath rusq-sql-$VERSION@<target>.vsix -p <marketplace-token>
  npx ovsx publish rusq-sql-$VERSION@<target>.vsix -p <open-vsx-token>
EOF
	exit 0
fi

# A rejected duplicate is a success for us: the version is already live.
publish_one() {
	local tool="$1" vsix="$2" out
	shift 2
	out="$("$@" 2>&1)" && {
		printf '%s\n' "$out"
		return 0
	}
	if printf '%s' "$out" | grep -qiE 'already (exists|published)|is already|has already been published'; then
		echo "==> $tool publish $(basename "$vsix"): already published, skipping"
		return 0
	fi
	printf '%s\n' "$out" >&2
	return 1
}

if [ "$want_ovsx" = 1 ]; then
	# Open VSX needs the namespace (which must equal the publisher id) before
	# the first publish. Creating an existing namespace is a harmless error.
	echo "==> ovsx create-namespace $PUBLISHER"
	if ! out="$(npx --yes ovsx create-namespace "$PUBLISHER" -p "$OVSX_PAT" 2>&1)"; then
		if printf '%s' "$out" | grep -qi 'already'; then
			echo "    namespace already exists, continuing"
		else
			printf '%s\n' "$out" >&2
			exit 1
		fi
	else
		printf '%s\n' "$out"
	fi
fi

for vsix in "${VSIXES[@]}"; do
	name="$(basename "$vsix")"
	if [ "$want_marketplace" = 1 ]; then
		echo "==> vsce publish $name"
		publish_one vsce "$vsix" \
			npx --no-install vsce publish --packagePath "$vsix" -p "$VSCE_PAT"
	fi
	if [ "$want_ovsx" = 1 ]; then
		echo "==> ovsx publish $name"
		publish_one ovsx "$vsix" \
			npx --yes ovsx publish "$vsix" -p "$OVSX_PAT"
	fi
done
