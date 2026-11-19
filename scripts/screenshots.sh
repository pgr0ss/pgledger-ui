#!/usr/bin/env bash
# Captures screenshots of a running pgledger-ui with headless Chrome.
# Usage: scripts/screenshots.sh [out_dir, default ./screenshots]   env: BASE, WIDTH, HEIGHT, SCALE, DARK=1, CHROME
set -euo pipefail

BASE=${BASE:-http://127.0.0.1:8080}
OUT=${1:-screenshots}
WIDTH=${WIDTH:-1440}
HEIGHT=${HEIGHT:-900}
SCALE=${SCALE:-1}

find_chrome() {
	local c
	for c in "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" google-chrome google-chrome-stable chromium; do
		command -v "$c" && return
	done
	echo "Chrome not found; set CHROME" >&2
	return 1
}
CHROME=${CHROME:-$(find_chrome)}

mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
profile=$(mktemp -d)
trap 'rm -rf "$profile"' EXIT

first_link() {
	local html
	html=$(curl -fsS "$BASE$1")
	grep -oE "href=\"$2/[^\"?]+\"" <<<"$html" | sed -n 1p | cut -d'"' -f2 ||
		{ echo "no $2/ link on $BASE$1" >&2; return 1; }
}

account=$(first_link "/transfers" "/accounts")
transfer=$(first_link "/transfers" "/transfers")

# name|path
shots=(
	"01-home|/"
	"02-search|/search?q=user001"
	"03-account-entries|$account"
	"04-transfer|$transfer"
	"05-trace-crosstab|/trace?key=payment_id&value=p_00050"
	"06-trace-inout|/trace?key=payment_id&value=p_00050&view=inout"
)

# Blink's PreferredColorScheme: 0 dark, 1 light.
flags=(--blink-settings=preferredColorScheme=1)
[[ ${DARK:-} == 1 ]] && flags=(--force-dark-mode --blink-settings=preferredColorScheme=0)

for shot in "${shots[@]}"; do
	IFS='|' read -r name path <<<"$shot"
	file="$OUT/$name.png"
	curl -fsS -o /dev/null "$BASE$path"
	"$CHROME" --headless=new --disable-gpu --disable-crash-reporter --no-first-run \
		--user-data-dir="$profile" --hide-scrollbars \
		--window-size="$WIDTH,$HEIGHT" --force-device-scale-factor="$SCALE" \
		"${flags[@]}" \
		--screenshot="$file" "$BASE$path" >/dev/null 2>&1
	echo "$file  <-  $BASE$path"
done
