#!/bin/sh
# Warm the public pages of a freshly started production frontend.
#
# `next build` runs without the backend, so the prerendered HTML lacks the
# platform theme (data-theme, read from /api/v1/system/ui-theme). Next
# regenerates a page on the first request once that HTML is 60 s old; this
# script makes those first requests instead of a visitor, until every page
# carries the theme. Usage: scripts/warm-frontend.sh [base-url]
set -u
BASE="${1:-http://127.0.0.1:18000}"
PAGES="/ /gioi-thieu /login /register /dieu-khoan /chinh-sach-bao-mat /ho-tro /onboarding/workspace /word-login /extension-login"
i=0
while [ "$i" -lt 60 ]; do
  i=$((i + 1))
  for p in $PAGES; do curl -s -o /dev/null "$BASE$p"; done
  missing=""
  for p in $PAGES; do
    curl -s "$BASE$p" | head -c 200 | grep -q 'data-theme=' || missing="$missing $p"
  done
  if [ -z "$missing" ]; then
    echo "warm: every page carries the theme (round $i)"
    exit 0
  fi
  sleep 4
done
echo "warm: still without a theme:$missing (is the backend up?)" >&2
exit 1
