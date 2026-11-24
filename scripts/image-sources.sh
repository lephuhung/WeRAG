#!/usr/bin/env bash
# The repo paths each image built from this repo is made of, and the source
# stamp of an image: a hash of the git blobs (mode, hash, path) of the files
# those paths select at HEAD, which changes exactly when one of them does.
# The images carry it as the label werag.src-stamp (build arg
# WERAG_SRC_STAMP, scripts/build-image.sh); scripts/check-images.sh compares
# the running containers' label with HEAD.
#
#   image-sources.sh list                    services, one per line
#   image-sources.sh paths SERVICE           its path patterns
#   image-sources.sh files SERVICE           the files they select at HEAD
#   image-sources.sh stamp SERVICE           stamp of the working tree: HEAD's,
#                                            plus "-dirty-<hash>" when a selected
#                                            file has uncommitted changes
#   image-sources.sh stamp --head SERVICE    stamp of HEAD only
#   image-sources.sh container SERVICE       container_name in docker-compose.yml
#                                            ("" when compose names it)
#   image-sources.sh profile SERVICE         compose profile ("" for none)
#   image-sources.sh env SERVICE             the build-arg variable name
#
# A path pattern is a plain path (that file, or every file under that
# directory), or a glob when it holds * ? or [ (* and ? stop at "/", "**/"
# spans directories: "internal/types/legal_*.go", "**/*_test.go"). A
# leading "!" excludes what the pattern selects. A plain path selecting
# nothing (a typo in the table) is reported on stderr and stamped as
# "missing"; a glob selecting nothing is not an error. (git ls-tree takes
# no glob/exclude pathspec magic on git 2.43, so the patterns are matched
# here, on its full listing.)
#
# WERAG_REPO overrides the repository (default: this script's parent).
#
# The table is read off each Dockerfile's COPY lines and build context (and
# .dockerignore): keep it in step when a Dockerfile changes. A path listed
# too widely makes an image look stale after an unrelated change; a path
# missing makes a real change go unseen. The Go images leave out *_test.go.
# app lists everything its binary links (go list -deps ./cmd/server),
# internal/ as a whole: honest for a full backend image. vbpl-sync is
# narrowed on purpose to the importer's own code and the legal code it
# drives (go list -deps would also pull in all of internal/application/
# service, making the image stale after almost every backend commit): a
# behaviour change outside these paths needs legalcatalog.ImporterRevision
# bumped, which stays the second net.
set -euo pipefail

REPO=${WERAG_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}

# service | container_name | compose profile | path patterns (space separated)
TABLE=$(cat <<'EOF'
app|WeKnora-app||docker/Dockerfile.app go.mod go.sum Makefile VERSION LICENSE THIRD_PARTY_NOTICES.md licenses cmd/server cmd/download internal docreader/client docreader/proto docs/docs.go domain-packs examples/skills config scripts migrations dataset/samples patches/browserskill !**/*_test.go !**/testdata/**
sandbox|WeKnora-sandbox|full|docker/Dockerfile.sandbox docker/sandbox-pty-prompt.sh
docreader|WeKnora-docreader||docker/Dockerfile.docreader docreader packages
frontend|WeKnora-frontend||frontend-next !frontend-next/next-env.d.ts
sherpa-asr|WeKnora-sherpa-asr|asr|docker/sherpa-asr/Dockerfile docker/sherpa-asr/server.py docker/sherpa-asr/capu.py
screenai-ocr|WeKnora-screenai-ocr|screenai|docker/screenai-ocr/Dockerfile docker/screenai-ocr/server.py docker/screenai-ocr/screenai.py docker/screenai-ocr/fetch_component.py docker/screenai-ocr/entrypoint.sh
backup|WeKnora-backup|backup|deploy/backup/Dockerfile deploy/backup/backup.sh deploy/backup/entrypoint.sh deploy/backup/restore.sh
vbpl-sync|WeKnora-vbpl-sync|vbpl-sync|deploy/vbpl-sync/Dockerfile deploy/vbpl-sync/sync.sh deploy/vbpl-sync/entrypoint.sh go.mod go.sum cmd/legal-catalog-import internal/legalcatalog internal/vbpl scripts/legal_catalog/crawl_vbpl.py internal/types/legal_*.go internal/types/interfaces/legal_*.go internal/application/repository/legal_*.go internal/application/service/legal_*.go internal/vietnamese_legal/*.go internal/types/json.go internal/types/knowledge.go !**/*_test.go !**/testdata/**
dcs-sync|WeKnora-dcs-sync|dcs-sync|deploy/dcs-sync/Dockerfile deploy/dcs-sync/sync.sh deploy/dcs-sync/entrypoint.sh deploy/dcs-sync/run_track.py scripts/dangcongsan/dcs_fetch.py scripts/dangcongsan/dcs_classify.py scripts/dangcongsan/dcs_text.py scripts/dangcongsan/dcs_build.py scripts/dangcongsan/dcs_push.py
embed-rerank|hrag-embed-rerank|airag-models|services/embed-rerank/Dockerfile services/embed-rerank/requirements.txt services/embed-rerank/app.py docker/adapters/openai_embed_rerank.py
tvpl||tvpl|docker/tvpl/Dockerfile docker/tvpl/entrypoint.sh scripts/tvpl/tvpl_fetch.py scripts/tvpl/tvpl_convert.py scripts/tvpl/tvpl_push.py scripts/tvpl/test_tvpl_convert.py
EOF
)

row() { # row SERVICE → the table line, or exit 2
	local line
	line=$(grep -E "^$1\|" <<<"$TABLE" || true)
	[ -n "$line" ] || { echo "image-sources.sh: unknown service '$1'" >&2; exit 2; }
	echo "$line"
}
field() { row "$1" | cut -d'|' -f"$2"; }

paths_of() { field "$1" 4 | tr ' ' '\n' | grep -v '^$'; }

# select_paths PATTERNS_FILE [missing]: keep the input lines whose second
# tab field (the path) the patterns select (git ls-tree's "mode type
# hash<TAB>path"); with "missing", every plain include selecting nothing is
# printed as "missing<TAB><path>" (and reported on stderr).
select_paths() {
	awk -F'\t' -v field=2 -v missing="${2:-}" -v pf="$1" '
	function rx(p,   out, i, c, n) {
		if (p !~ /[*?[]/) { gsub(/[.^$+(){}|\\]/, "\\\\&", p); return "^" p "(/|$)" }
		out = ""; n = length(p)
		for (i = 1; i <= n; i++) {
			c = substr(p, i, 1)
			if (c == "*" && substr(p, i, 3) == "**/") { out = out "(.*/)?"; i += 2 }
			else if (c == "*" && substr(p, i, 2) == "**") { out = out ".*"; i++ }
			else if (c == "*") out = out "[^/]*"
			else if (c == "?") out = out "[^/]"
			else if (c ~ /[.^$+(){}|\\]/) out = out "\\" c
			else out = out c
		}
		return "^" out "$"
	}
	BEGIN {
		while ((getline p < pf) > 0) {
			if (substr(p, 1, 1) == "!") { nex++; ex[nex] = rx(substr(p, 2)) }
			else { nin++; inc[nin] = rx(p); plain[nin] = (p !~ /[*?[]/); name[nin] = p }
		}
	}
	{
		path = $field
		for (i = 1; i <= nex; i++) if (path ~ ex[i]) next
		hit = 0
		for (i = 1; i <= nin; i++) if (path ~ inc[i]) { seen[i] = 1; hit = 1 }
		if (hit) print
	}
	END {
		if (missing == "") exit
		for (i = 1; i <= nin; i++) if (plain[i] && !seen[i]) {
			print "missing\t" name[i]
			print "image-sources.sh: " name[i] " selects no file at HEAD" > "/dev/stderr"
		}
	}'
}

patterns_file() { # patterns_file SERVICE → a temp file with its patterns
	local f
	f=$(mktemp)
	paths_of "$1" >"$f"
	echo "$f"
}

head_files() { # head_files PATTERNS_FILE → selected ls-tree lines, sorted
	git -C "$REPO" ls-tree -r --full-tree HEAD | select_paths "$1" missing | LC_ALL=C sort
}

stamp() { # stamp [--head] SERVICE
	local head=0
	[ "$1" = "--head" ] && { head=1; shift; }
	local svc=$1 pf base
	row "$svc" >/dev/null
	pf=$(patterns_file "$svc")
	base=$(head_files "$pf" | sha256sum | cut -c1-16)
	if [ "$head" = 0 ]; then
		# changed paths of the working tree (porcelain v1 -z: "XY path", a
		# rename adds the original path as the next field)
		local changed
		changed=$(git -C "$REPO" status --porcelain=v1 -z --untracked-files=all | awk -v RS='\0' '
			skip { skip = 0; print "x\t" $0; next }
			{ st = substr($0, 1, 2); print st "\t" substr($0, 4); if (st ~ /[RC]/) skip = 1 }' |
			select_paths "$pf" | LC_ALL=C sort -u)
		if [ -n "$changed" ]; then
			local files=()
			while IFS=$'\t' read -r _ p; do files+=("$p"); done <<<"$changed"
			base="$base-dirty-$({ echo "$changed"; git -C "$REPO" diff HEAD -- "${files[@]}"; } | sha256sum | cut -c1-8)"
		fi
	fi
	rm -f "$pf"
	echo "$base"
}

case "${1:-}" in
list) cut -d'|' -f1 <<<"$TABLE" ;;
paths) paths_of "${2:?service}" ;;
files)
	row "${2:?service}" >/dev/null
	pf=$(patterns_file "$2")
	head_files "$pf" | cut -f2
	rm -f "$pf"
	;;
stamp) shift; stamp "$@" ;;
container) field "${2:?service}" 2 ;;
profile) field "${2:?service}" 3 ;;
env) row "${2:?service}" >/dev/null; echo "WERAG_SRC_STAMP_$(tr 'a-z-' 'A-Z_' <<<"$2")" ;;
*)
	sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//' >&2
	exit 2
	;;
esac
