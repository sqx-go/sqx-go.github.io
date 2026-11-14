#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="output"
SQX_BINARY="$REPO_ROOT/$OUTPUT_DIR/sqx"
CACHE_DB="$OUTPUT_DIR/cache.db"

run_online=false
[[ "${1:-}" == "--online" ]] && run_online=true

title() {
  printf '\n\033[1;36m== %s ==\033[0m\n' "$1"
}

example() {
  local command
  command="$(cat)"
  printf '\033[33m$ %s\033[0m\n' "$command"
  eval "$command" < /dev/null
}

sqx() {
  case "${1:-}" in
    import) shift; "$SQX_BINARY" import -cache-db "$CACHE_DB" "$@" ;;
    prefetch | cache) local command="$1"; shift; "$SQX_BINARY" "$command" -cache-db "$CACHE_DB" -o table "$@" ;;
    tables | schema) local command="$1"; shift; "$SQX_BINARY" "$command" -o table "$@" ;;
    *) "$SQX_BINARY" -cache-db "$CACHE_DB" -o table "$@" ;;
  esac
}

build_binary() {
  CGO_ENABLED=0 go build -o "$SQX_BINARY" ./sqx
}

reset_output_dir() {
  rm -rf "$OUTPUT_DIR"
  mkdir -p "$OUTPUT_DIR"
}

print_conventions() {
  printf 'Commands run from %s.\n' "$REPO_ROOT"
  printf 'sqx is %s, run with -cache-db %s and -o table (a later -o overrides it).\n' "$OUTPUT_DIR/sqx" "$CACHE_DB"
}

show_stdin_and_input_formats() {
  title "1. CSV on stdin"
  example <<'CMD'
cat data/users.csv | sqx "SELECT country, COUNT(*) AS customers FROM stdin GROUP BY country ORDER BY customers DESC"
CMD

  title "2a. TSV"
  example <<'CMD'
cat data/metrics.tsv | sqx "SELECT service, endpoint, latency_ms, status FROM stdin WHERE status >= 500"
CMD

  title "2b. JSONL with nested objects"
  example <<'CMD'
cat data/events.jsonl | sqx "SELECT id, type, amount, json_extract(meta, '$.device') AS device, json_extract(meta, '$.sku') AS sku, tags FROM stdin"
CMD

  title "2c. JSON array"
  example <<'CMD'
echo '[{"id":1,"zip":"01234"},{"id":2,"active":true}]' | sqx "SELECT id, zip, active, typeof(zip) AS zip_type FROM stdin"
CMD

  title "2d. Plain lines"
  example <<'CMD'
cat data/urls.txt | sqx -i lines "SELECT linenum, url FROM stdin"
CMD

  title "2e. Headerless CSV"
  example <<'CMD'
cat data/headerless.csv | sqx -no-header "SELECT c2 AS city, SUM(c3) AS orders FROM stdin GROUP BY c2"
CMD

  title "2f. Sanitised column names"
  example <<'CMD'
cat data/messy_headers.csv | sqx "PRAGMA table_info(stdin)"
CMD

  title "2g. Missing markers like N/A become NULL, so the column stays numeric"
  example <<'CMD'
cat data/metrics.tsv | sqx "SELECT service, endpoint, latency_ms FROM stdin WHERE latency_ms > 300 ORDER BY latency_ms DESC"
CMD
}

show_joins_and_output_formats() {
  title "3. Joining CSV and JSONL with -f"
  example <<'CMD'
sqx -f users=data/users.csv -f events=data/events.jsonl "SELECT u.name, u.plan, COUNT(e.id) AS events, ROUND(SUM(e.amount),2) AS revenue FROM users u LEFT JOIN events e ON e.user_id = u.id GROUP BY u.id ORDER BY revenue DESC"
CMD

  local format
  for format in csv tsv json jsonl md raw; do
    title "4. Output format: $format"
    example <<CMD
sqx -f u=data/users.csv -o $format "SELECT id, name, plan FROM u LIMIT 3"
CMD
  done

  title "4. Piping sqx into sqx"
  example <<'CMD'
sqx -f u=data/users.csv -o csv "SELECT * FROM u WHERE plan != 'free'" | sqx "SELECT country, COUNT(*) AS paying_customers FROM stdin GROUP BY country"
CMD
}

show_url_functions() {
  title "5. URL functions"
  example <<'CMD'
cat data/urls.txt | sqx -i lines "SELECT url, url_scheme(url) AS scheme, url_host(url) AS host, url_path(url) AS path, url_norm(url) AS normalized FROM stdin"
CMD
  example <<'CMD'
sqx "SELECT url_join('https://a.com/dir/page.html', '../img.png') AS joined, url_query('https://x.com/?id=42&sort=asc', 'id') AS id, url_ext('https://cdn.com/assets/app.min.js?v=2') AS ext"
CMD
}

show_html_functions() {
  title "6a. Title, meta, headings"
  example <<'CMD'
sqx -f pages=data/pages.jsonl "SELECT url_path(url) AS page, html_title(html) AS title, html_meta(html, 'description') AS description, html_meta(html, 'og:image') AS og_image, html_tag_texts(html, 'h2') AS h2 FROM pages"
CMD

  title "6b. Attributes"
  example <<'CMD'
sqx -f pages=data/pages.jsonl "SELECT url_path(url) AS page, html_attr(html, 'script', 'src') AS scripts, html_attr(html, 'img', 'alt') AS image_alts FROM pages"
CMD

  title "6c. Link graph"
  example <<'CMD'
sqx -f pages=data/pages.jsonl "SELECT url_path(p.url) AS page, url_join(p.url, link.value) AS target, url_host(url_join(p.url, link.value)) AS target_host FROM pages p, json_each(html_links(p.html)) AS link"
CMD

  title "6d. Outbound domains"
  example <<'CMD'
sqx -f pages=data/pages.jsonl "SELECT url_host(link.value) AS external_host, COUNT(*) AS links FROM pages p, json_each(html_links(p.html)) AS link WHERE link.value LIKE 'http%' AND url_host(link.value) != url_host(p.url) GROUP BY external_host ORDER BY links DESC"
CMD

  title "6e. Duplicate content"
  example <<'CMD'
sqx -f pages=data/pages.jsonl "SELECT substr(sha256(html_text(html)), 1, 12) AS content_hash, COUNT(*) AS pages, GROUP_CONCAT(url_path(url), ' | ') AS urls FROM pages GROUP BY content_hash HAVING pages > 1"
CMD
}

show_regex_and_hashing() {
  title "7a. Emails with re_extract_all"
  example <<'CMD'
sqx -f pages=data/pages.jsonl "SELECT DISTINCT url_path(p.url) AS page, m.value AS email FROM pages p, json_each(re_extract_all(p.html, '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}')) AS m"
CMD

  title "7b. Tracking IDs and leaked keys"
  example <<'CMD'
sqx -f pages=data/pages.jsonl "SELECT url_path(url) AS page, re_extract(html, '(UA-[0-9]+-[0-9]+|G-[A-Z0-9]+)', 1) AS tracking_id, re_extract(html, 'api_key=([A-Za-z0-9_]+)', 1) AS leaked_key, ROUND(shannon_entropy(re_extract(html, 'api_key=([A-Za-z0-9_]+)', 1)), 2) AS key_entropy FROM pages WHERE html REGEXP 'api_key=|UA-|G-'"
CMD

  title "7c. Hashing and entropy"
  example <<'CMD'
sqx "SELECT sha256('hello') AS sha, shannon_entropy('aaaaaaaa') AS low, ROUND(shannon_entropy('Zx9Qp2Lm7Rt4Vb8N'), 2) AS high"
CMD
}

show_security_log_analysis() {
  title "8a. High-entropy tokens from public IPs"
  example <<'CMD'
cat data/access_log.csv | sqx "SELECT client_ip, url_path(request_url) AS path, url_query(request_url, 'token') AS token, ROUND(shannon_entropy(url_query(request_url, 'token')), 2) AS token_entropy FROM stdin WHERE NOT ip_is_private(client_ip) AND url_query(request_url, 'token') IS NOT NULL ORDER BY token_entropy DESC"
CMD

  title "8b. Attack signatures"
  example <<'CMD'
cat data/access_log.csv | sqx "SELECT ts, client_ip, status, user_agent, request_url FROM stdin WHERE NOT ip_is_private(client_ip) AND (request_url REGEXP '(?i)(\.\./|%27|or%201%3d1|/etc/passwd)' OR user_agent REGEXP '(?i)sqlmap|nikto|python-requests')"
CMD

  title "8c. Traffic split"
  example <<'CMD'
cat data/access_log.csv | sqx "SELECT CASE ip_is_private(client_ip) WHEN 1 THEN 'internal' ELSE 'external' END AS source, CASE WHEN url_ext(request_url) IN ('.js','.css','.png','.jpg') THEN 'static' ELSE 'dynamic' END AS kind, COUNT(*) AS requests, SUM(bytes) AS bytes FROM stdin GROUP BY 1, 2"
CMD
}

show_workspace() {
  title "11a. Import into a persistent workspace"
  example <<'CMD'
sqx import -db output/project.db -t users data/users.csv
CMD
  example <<'CMD'
sqx import -db output/project.db -t users -replace data/users.csv
CMD
  example <<'CMD'
sqx import -db output/project.db -t events data/events.jsonl
CMD
  example <<'CMD'
sqx import -db output/project.db -t sitemaps data/sitemaps.csv
CMD
  example <<'CMD'
sqx import -db output/project.db -t targets data/targets.jsonl
CMD
  example <<'CMD'
cat data/metrics.tsv | sqx import -db output/project.db -t metrics
CMD
  example <<'CMD'
sqx import -db output/project.db -t urls -i lines data/urls.txt
CMD

  title "11b. tables and schema"
  example <<'CMD'
sqx tables -db output/project.db
CMD
  example <<'CMD'
sqx schema -db output/project.db
CMD

  title "11c. Save a query result with -save (re-running replaces the table)"
  example <<'CMD'
sqx -db output/project.db -save big_spenders "SELECT u.name, u.country, SUM(e.amount) AS total FROM users u JOIN events e ON e.user_id = u.id GROUP BY u.id HAVING total > 40"
CMD
  example <<'CMD'
sqx -db output/project.db -save big_spenders "SELECT u.name, u.country, SUM(e.amount) AS total FROM users u JOIN events e ON e.user_id = u.id GROUP BY u.id HAVING total > 100"
CMD

  title "11e. INSERT ... RETURNING prints the new row"
  example <<'CMD'
sqx -db output/project.db "INSERT INTO users (id, name, country, plan) VALUES (7, 'Gita Iyer', 'IN', 'free') RETURNING id, name"
CMD
}

show_live_web() {
  title "9a. Prefetch URLs concurrently"
  example <<'CMD'
cat data/urls.txt | sqx prefetch -c 8 -timeout 10s
CMD

  title "9b. HTTP audit from cache"
  example <<'CMD'
cat data/urls.txt | sqx -i lines -offline "SELECT url, http_status(url) AS status, http_time_ms(url) AS ms, http_final_url(url) AS final_url, http_header(url, 'content-type') AS content_type, http_size(url) AS bytes, html_title(url) AS title, http_error(url) AS error FROM stdin ORDER BY status"
CMD

  title "9c. Live outbound link graph"
  example <<'CMD'
cat data/urls.txt | sqx -i lines -offline "SELECT url_host(stdin.url) AS source_host, url_host(link.value) AS target_host, COUNT(*) AS links FROM stdin, json_each(html_links(stdin.url)) AS link WHERE url_host(link.value) != url_host(stdin.url) GROUP BY 1, 2 ORDER BY links DESC LIMIT 10"
CMD

  title "9d. Duplicate live pages"
  example <<'CMD'
cat data/urls.txt | sqx -i lines -offline "SELECT substr(sha256(html_text(url)), 1, 12) AS hash, COUNT(*) AS pages, GROUP_CONCAT(url, ' | ') AS urls FROM stdin WHERE http_status(url) = 200 GROUP BY hash HAVING pages > 1"
CMD

  title "10. DNS and email security"
  example <<'CMD'
cat data/companies.csv | sqx "SELECT company, domain, dns_ip(domain) AS ip, CASE WHEN dns_txt_json(domain) LIKE '%v=spf1%' THEN 'yes' ELSE 'MISSING' END AS spf, CASE WHEN dns_txt_json('_dmarc.' || domain) LIKE '%v=DMARC1%' THEN 'yes' ELSE 'MISSING' END AS dmarc FROM stdin"
CMD

  title "11d. Broken-link report"
  example <<'CMD'
sqx -db output/project.db -save broken_links "SELECT t.category, t.owner, s.loc AS url, http_status(s.loc) AS status, http_error(s.loc) AS error FROM sitemaps s JOIN targets t ON url_host(s.loc) = t.domain WHERE http_status(s.loc) >= 400 OR http_status(s.loc) = 0"
CMD
  example <<'CMD'
sqx -db output/project.db -o md "SELECT * FROM broken_links ORDER BY status DESC" > output/broken_links.md && cat output/broken_links.md
CMD

  title "12. Cache inspection"
  example <<'CMD'
sqx cache stats
CMD
  example <<'CMD'
sqx cache query "SELECT lookup_key, records_json FROM dns_cache ORDER BY lookup_key"
CMD
}

cd "$REPO_ROOT"
reset_output_dir
build_binary
print_conventions
show_stdin_and_input_formats
show_joins_and_output_formats
show_url_functions
show_html_functions
show_regex_and_hashing
show_security_log_analysis
show_workspace
if $run_online; then
  show_live_web
else
  printf '\nSkipped network examples (sections 9, 10, 11d, 12). Run with --online to include them.\n'
fi
