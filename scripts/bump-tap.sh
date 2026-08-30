#!/bin/sh
# Regenerate the Homebrew tap formula for a release and push it to
# meowkey-dev/homebrew-tap, so `brew install meowkey-dev/tap/analog` tracks it.
#
#   scripts/bump-tap.sh v0.5.0
#
# Reads the release's SHA256SUMS (same source the installer verifies against),
# rewrites Formula/analog.rb wholesale, and pushes. With TAP_TOKEN set (CI), the
# push authenticates as that token; otherwise your git credentials are used.
set -eu

REPO=meowkey-dev/analog
TAP=meowkey-dev/homebrew-tap

ver=${1:?usage: bump-tap.sh vX.Y.Z}
case $ver in v[0-9]*) ;; *) echo "bump-tap.sh: tag must look like v0.4.0" >&2; exit 1 ;; esac
version=${ver#v}

sums=$(mktemp)
trap 'rm -f "$sums"' EXIT
curl -fsSL --retry 3 "https://github.com/$REPO/releases/download/$ver/SHA256SUMS" -o "$sums"

sum() { awk -v asset="$1" '$2 == asset { print $1; found = 1 } END { exit !found }' "$sums"; }
for asset in analog-darwin-arm64 analog-darwin-amd64 analog-linux-arm64 analog-linux-amd64; do
    sum "$asset.tar.gz" >/dev/null # every platform the formula covers must be in the sums
done

work=$(mktemp -d)
trap 'rm -rf "$work" "$sums"' EXIT
if [ -n "${TAP_TOKEN:-}" ]; then
    git -C "$work" clone -q --depth 1 "https://x-access-token:$TAP_TOKEN@github.com/$TAP.git" tap
    name=github-actions[bot]
    email=41898282+github-actions[bot]@users.noreply.github.com
else
    git -C "$work" clone -q --depth 1 "git@github.com:$TAP.git" tap
    name=$(git config user.name)
    email=$(git config user.email)
fi
git -C "$work/tap" config user.name "$name"
git -C "$work/tap" config user.email "$email"

# The formula is generated, not hand-edited: version, urls and checksums all
# move together, and the sums come from the release rather than local builds.
cat > "$work/tap/Formula/analog.rb" <<EOF
class Analog < Formula
  desc "A shared canvas for one human and their agents"
  homepage "https://github.com/$REPO"
  license "Apache-2.0"
  version "$version"

  livecheck do
    url "https://github.com/$REPO/releases"
    strategy :github_latest
  end

  # The releases carry prebuilt Go binaries per platform, so the formula points
  # at the right archive directly instead of building from source.
  on_macos do
    on_arm do
      url "https://github.com/$REPO/releases/download/$ver/analog-darwin-arm64.tar.gz"
      sha256 "$(sum analog-darwin-arm64.tar.gz)"
    end
    on_intel do
      url "https://github.com/$REPO/releases/download/$ver/analog-darwin-amd64.tar.gz"
      sha256 "$(sum analog-darwin-amd64.tar.gz)"
    end
  end
  on_linux do
    on_arm do
      url "https://github.com/$REPO/releases/download/$ver/analog-linux-arm64.tar.gz"
      sha256 "$(sum analog-linux-arm64.tar.gz)"
    end
    on_intel do
      url "https://github.com/$REPO/releases/download/$ver/analog-linux-amd64.tar.gz"
      sha256 "$(sum analog-linux-amd64.tar.gz)"
    end
  end

  def install
    bin.install "analog", "analog-server", "analog-mcp"
  end

  test do
    assert_match "Run the Analog API", shell_output("#{bin}/analog-server --help")
    system "#{bin}/analog", "--help"
    # analog-mcp has no flags; an initialize round-trip proves it runs.
    assert_match "2024-11-05",
      pipe_output("#{bin}/analog-mcp", '{"jsonrpc":"2.0","id":1,"method":"initialize"}')
  end
end
EOF

git -C "$work/tap" add -A
if git -C "$work/tap" diff --cached --quiet; then
    echo "tap already at $ver"
    exit 0
fi
git -C "$work/tap" commit -qm "analog $version

Signed-off-by: $name <$email>"
git -C "$work/tap" push -q origin HEAD:main
echo "tap updated to $version"
