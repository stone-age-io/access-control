#!/usr/bin/env bash
# Build the GitHub release body for a tag, and print it to stdout.
#
# Ported from the platform's script of the same name, for the same reason:
# goreleaser's own changelog is a list of commit subjects, and this repo's
# reasoning lives in CHANGELOG.md as paragraphs. The part an integrator needs
# before upgrading a site -- a new field to set, a NATS permission to grant, a
# database that has to start fresh -- exists nowhere in the commit log. So the
# release body is the changelog section for the tag, wrapped in install prose.
#
# Usage:  scripts/release-notes.sh v0.1.0
#
# A FINAL tag requires its own `## [X.Y.Z]` section and fails without one, so a
# release cannot ship with an empty body because someone forgot to promote
# `## [Unreleased]`. A PRERELEASE tag (v0.1.1-rc1) reads `## [Unreleased]`
# instead, because that is where the content for an unreleased version is by
# definition -- an rc has no section of its own and never will.
set -euo pipefail

TAG="${1:-}"
if [ -z "$TAG" ]; then
  echo "usage: $0 <tag>   e.g. $0 v0.1.0" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHANGELOG="$ROOT/CHANGELOG.md"

VERSION="${TAG#v}"
case "$TAG" in
  *-*) HEADING='## [Unreleased]' ; KIND='prerelease' ;;
  *)   HEADING="## [$VERSION]"   ; KIND='release' ;;
esac

# Everything between this heading and the next `## ` heading, with surrounding
# blank lines trimmed. awk rather than sed: a section can contain `#` inside
# fenced code blocks, so the boundary has to be anchored to the start of a line
# and to the two-hash level exactly.
SECTION="$(
  awk -v want="$HEADING" '
    index($0, want) == 1 && substr($0, length(want) + 1, 1) ~ /^[ ]?$/ { grab = 1; next }
    grab && /^## / { exit }
    grab { print }
  ' "$CHANGELOG"
)"

# Trim leading and trailing blank lines.
SECTION="$(printf '%s\n' "$SECTION" | sed -e '/./,$!d' -e ':a' -e '/^\n*$/{$d;N;ba' -e '}')"

if [ -z "$(printf '%s' "$SECTION" | tr -d '[:space:]')" ]; then
  echo "release-notes: no content under '$HEADING' in CHANGELOG.md" >&2
  echo "release-notes: a $KIND tag needs that section filled in before tagging." >&2
  exit 1
fi

cat <<EOF
## stone-access $TAG

Two binaries, released together and downloaded separately. They share nothing
but NATS (with JetStream), and both must use the same \`subjects.app\`.

- **\`accessd\`**: the central service. PocketBase, the management console and
  the badge, the policy mirror to NATS KV, and the event projection. One per
  deployment.
- **\`access-controller\`**: the edge controller. Decides taps locally and
  drives the locks and door inputs. One per box beside the doors;
  \`linux_arm64\` is the build for the KinCony CM4 and CM5 boards.

Each archive carries its annotated example config at the path the binary loads
by default. It points at \`nats.example.com\` and names a creds file, so edit it
before the first run. A new install has no console operator, so create one
before signing in:

\`\`\`
./accessd migrate up
./accessd operator upsert you@example.com 'a-long-password'
./accessd serve                                   # console at http://127.0.0.1:8090/
./access-controller -config config/controller.yaml
\`\`\`

---

$SECTION

---

Pre-1.0: treat a minor version as potentially breaking, and pin what you
deploy. Run the same version of \`accessd\` and \`access-controller\`.

**Full changelog**: https://github.com/stone-age-io/access-control/commits/$TAG
EOF
