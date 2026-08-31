#!/usr/bin/env bash
# Report drift between OnRamp's copy of the target contract and Qniform's own.
#
# Deliberately out of band. The vendored copy under targets/ is what every Run
# validates against and what its recorded spec version resolves to; nothing in a
# Run reads Qniform's repository. Keeping the two in step is a decision someone
# makes, not something that happens silently between runs.
set -uo pipefail
cd "$(dirname "$0")"

VENDORED="targets/qniform/openapi.yaml"
UPSTREAM="${QNIFORM_SPEC:-$HOME/Code/qniform6/openapi.yaml}"

if [ ! -f "$UPSTREAM" ]; then
    echo "upstream not found at $UPSTREAM (set QNIFORM_SPEC)" >&2
    exit 2
fi

if diff -q "$VENDORED" "$UPSTREAM" >/dev/null; then
    echo "in step with $UPSTREAM"
    exit 0
fi

echo "DRIFT between $VENDORED and $UPSTREAM"
echo
diff -u "$VENDORED" "$UPSTREAM" | head -80
echo
echo "Adopting the change is a decision, not a refresh: it can alter what a Run"
echo "produces, and every Run since the last adoption was validated against the"
echo "copy above. To take it:"
echo
echo "    cp \"$UPSTREAM\" \"$VENDORED\" && bash build.sh"
exit 1
