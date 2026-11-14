# sqx: query CSV, JSON, logs and websites with SQL from the command line

**SQL for Unix streams and the web, in one static binary.**

[![Website](https://img.shields.io/badge/docs-sqx--go.github.io-ea580c)](https://sqx-go.github.io/)
[![License: GPL v2+](https://img.shields.io/badge/license-GPL--2.0--or--later-blue)](LICENSE)
[![Go](https://img.shields.io/badge/built%20with-Go-00ADD8)](https://go.dev/)
![Platforms](https://img.shields.io/badge/platforms-macOS%20%7C%20Linux%20%7C%20Windows%20%7C%20FreeBSD-lightgrey)

**📖 Documentation: [sqx-go.github.io](https://sqx-go.github.io/)** · [Install](https://sqx-go.github.io/install/) · [Quick start](https://sqx-go.github.io/quick-start/) · [SQL functions](https://sqx-go.github.io/functions/) · [Cookbook](https://sqx-go.github.io/cookbook/)

`sqx` is a free, open-source **SQL command-line tool** that loads CSV, TSV, JSON, JSON Lines (NDJSON) and plain-text lines into **SQLite** tables, then lets you query them with SQL. It also adds SQL functions for HTTP fetching (with a persistent cache), HTML extraction, DNS lookups, URL parsing, regex, hashing and entropy, so you can do **web scraping, URL auditing, SEO checks and log analysis in plain SQL**.

You don't need a database server, credentials or throwaway Python scripts to join CSVs, audit a list of URLs or scan pages for leaked keys.

```bash
cat users.csv | sqx "SELECT country, COUNT(*) FROM stdin GROUP BY country"
```

```bash
cat urls.txt | sqx -i lines "SELECT url, http_status(url), html_title(url) FROM stdin"
```

### What people use sqx for

- **Query CSV files with SQL** from the terminal, without importing them into a database
- **Query JSON and JSON Lines with SQL**, including nested fields via `json_extract`
- **Join CSV and JSON files** in a single SQL query
- **Convert CSV to JSON, JSON to CSV, or data to Markdown tables**
- **Import CSV and JSON into SQLite** database files
- **Bulk-check HTTP status codes**, redirects and response headers for a list of URLs
- **Broken link checking** and **SEO audits**: titles, meta descriptions, H1s, image alt text
- **Scrape and parse HTML with SQL**: links, headings, attributes and text
- **Analyse access logs** for attack patterns, scanners and high-entropy tokens
- **Find leaked API keys, emails and tracking IDs** in web pages
- **Check SPF and DMARC records** and DNS for a list of domains

A lightweight, single-binary alternative to stitching together `csvkit`, `jq`, `q`, `textql`, `curl`, `pup` and `dig`.

---

## Contents

- [Features](#features)
- [Install](#install)
- [Quick start](#quick-start)
- [Key concepts](#key-concepts)
- [Commands](#commands)
- [Flags](#flags)
- [SQL function reference](#sql-function-reference)
- [Cookbook](#cookbook)
- [Building the binaries yourself](#building-the-binaries-yourself)
- [Documentation website](#documentation-website)
- [License](#license)

---

## Features

| | |
| :--- | :--- |
| **Any stream is a table** | CSV, TSV, JSON arrays, JSON Lines and plain lines. The format is detected automatically, column types are inferred, and messy headers are cleaned up. |
| **Join files on the fly** | Load any number of files or URLs with `-f name=path` and `JOIN` them in one query. |
| **Crawl once, query many times** | `http_*` and `html_*` functions share a persistent SQLite cache, so each URL is fetched once and every later query reads it from disk. |
| **Concurrent prefetch** | `sqx prefetch -c 25` warms the cache in parallel before you run slow per-row HTTP queries. |
| **HTML without a browser** | Get titles, meta tags, headings, links, attributes and plain text from fetched pages or stored HTML. |
| **DNS and security helpers** | A/TXT lookups (SPF, DMARC), private-IP checks, regex extraction, SHA-256 and Shannon entropy. |
| **Unix-friendly output** | `table` in a terminal and `csv` when piped, plus `tsv`, `json`, `jsonl`, `md` and `raw`. |
| **Optional persistence** | In memory by default; use `sqx import` and `-save` to build a SQLite workspace file. |
| **Nothing else to install** | One self-contained binary for macOS, Linux, Windows and FreeBSD. |

---

## Install

`sqx` is a single self-contained executable. There is nothing else to install: no runtime, no database server and no shared libraries.

### 1. Download

Open the [**GitHub Releases**](https://github.com/sqx-go/sqx-go.github.io/releases/latest) page, choose the latest release and download the file for your system:

| System | File |
| :--- | :--- |
| macOS, Apple Silicon (M1/M2/M3/M4) | `sqx-darwin-arm64` |
| macOS, Intel | `sqx-darwin-amd64` |
| Linux, x86-64 | `sqx-linux-amd64` |
| Linux, ARM64 (AWS Graviton, Raspberry Pi 4/5 64-bit) | `sqx-linux-arm64` |
| Linux, ARMv7 (Raspberry Pi 32-bit) | `sqx-linux-arm` |
| Linux, x86 32-bit | `sqx-linux-386` |
| Windows, x86-64 | `sqx-windows-amd64.exe` |
| Windows, ARM64 | `sqx-windows-arm64.exe` |
| FreeBSD, x86-64 | `sqx-freebsd-amd64` |
| FreeBSD, ARM64 | `sqx-freebsd-arm64` |

Not sure which one? Run `uname -sm` on macOS or Linux. On Windows, most PCs need `amd64`.

You can also download from the terminal:

```bash
curl -L -o sqx https://github.com/sqx-go/sqx-go.github.io/releases/latest/download/sqx-linux-amd64
```

### Verify the download (optional)

Each release includes a `SHA256SUMS` file. Download it into the same folder as the binary and run:

```bash
shasum -a 256 -c SHA256SUMS --ignore-missing
```

### 2. Put it on your PATH

**macOS / Linux / FreeBSD**

```bash
chmod +x sqx
sudo mv sqx /usr/local/bin/sqx
sqx version
```

On macOS, if you see *"cannot be opened because the developer cannot be verified"*, remove the download quarantine flag:

```bash
xattr -d com.apple.quarantine /usr/local/bin/sqx
```

**Windows (PowerShell)**

```powershell
mkdir $HOME\bin -Force
Move-Item .\sqx-windows-amd64.exe $HOME\bin\sqx.exe
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$HOME\bin", "User")
sqx version
```

Open a new terminal after changing `Path`.

### Install with Go

If you have [Go](https://go.dev/dl/) installed, you can skip the download and run:

```bash
go install github.com/sqx-go/sqx-go.github.io/sqx@latest
sqx version
```

This puts `sqx` in `$(go env GOPATH)/bin`, which needs to be on your `PATH`. To update, run the same command again.

### Updating and uninstalling

To update, download the new release and replace the file. To uninstall, delete the binary. Optionally also delete the web cache in `~/.cache/sqx` (Linux), `~/Library/Caches/sqx` (macOS) or `%LocalAppData%\sqx` (Windows).

---

## Quick start

These examples use the sample files in this repository's [`data/`](data/) folder. Clone or download the repository, then run them from its root:

```bash
# 1. Query a CSV piped on stdin
cat data/users.csv | sqx "SELECT country, COUNT(*) AS customers FROM stdin GROUP BY country"

# 2. Join a CSV with a JSON Lines file
sqx -f users=data/users.csv -f events=data/events.jsonl "
  SELECT u.name, COUNT(e.id) AS events, ROUND(SUM(e.amount), 2) AS revenue
  FROM users u LEFT JOIN events e ON e.user_id = u.id
  GROUP BY u.id ORDER BY revenue DESC"

# 3. Audit live URLs (fetched once, then cached)
cat data/urls.txt | sqx -i lines "
  SELECT url, http_status(url) AS status, http_final_url(url) AS final_url, html_title(url) AS title
  FROM stdin"
```

Don't have the sample files? Download any CSV or JSON file and point `sqx` at it. Every example works the same way.

---

## Key concepts

### Your data becomes tables

- Data piped into `sqx` becomes a table called `stdin` (rename it with `-t`).
- Each `-f name=path` becomes a table called `name`. With just `-f path`, the table is named after the file.
- **The format is detected automatically:** JSON array, JSON Lines, TSV, a plain list of URLs, or CSV. Force one with `-i csv|tsv|json|jsonl|lines`.
- **Column names are cleaned up:** `First Name` becomes `first_name`, `2023 Revenue ($)` becomes `c_2023_revenue`, and a second `Amount` becomes `amount_2`.
- **Numbers are recognised:** numeric columns become numbers, so `WHERE latency_ms > 300` works. Placeholders such as `N/A`, `NULL` or `-` become `NULL`, and values like `01234` keep their leading zero.
- **Nested JSON is kept as JSON text,** so SQLite's `json_extract` and `json_each` work on it.

### Fetch once, query as often as you like

Every web function (`http_*`, `html_*`, `dns_*`) shares a cache on disk. However many functions you call on a URL, it is downloaded only once, and later runs read it from the cache without touching the network.

| You want to… | Use |
| :--- | :--- |
| Work without a network | `-offline` |
| Refresh everything | `-force` |
| Treat entries older than an hour as stale | `-cache-ttl 1h` |
| Keep a separate cache per project | `-cache-db ./project-cache.db` |
| Warm the cache for many URLs quickly | `sqx prefetch -c 25` |

The default cache is in your user cache folder: `~/.cache/sqx` on Linux, `~/Library/Caches/sqx` on macOS and `%LocalAppData%\sqx` on Windows. Manage it with [`sqx cache`](#sqx-cache).

### Temporary by default, saved when you ask

- A plain query runs in memory, and its tables disappear when the command ends.
- Use `sqx import -db project.db …` to save files, stdin or URLs into a SQLite database file.
- Use `-db project.db -save <table> "<SQL>"` to save a query result. Running it again replaces the table.
- Tables from stdin and `-f` are never written into `-db`, so repeating a command never duplicates rows.

---

## Commands

| Command | Purpose |
| :--- | :--- |
| `sqx [flags] "<SQL>"` | Query stdin, `-f` files or a `-db` workspace |
| `sqx prefetch` | Fetch URLs concurrently into the cache |
| `sqx import` | Load files, stdin or URLs into a database file |
| `sqx cache` | Inspect, query or clear the HTTP/DNS cache |
| `sqx tables` / `sqx schema` | List tables with row counts, or show their `CREATE` statements |
| `sqx version` | Print the version |

Flags can go before or after positional arguments in every command.

### Query (default)

```bash
cat data.csv | sqx [flags] "<SQL>"
sqx -f users=users.csv -f events=events.jsonl "<SQL>"
sqx -db project.db "<SQL>"
```

Statements that don't return rows (`INSERT`, `UPDATE`, `CREATE`, …) run as writes, and you can separate several with `;`. `INSERT … RETURNING` prints the returned rows.

### `sqx prefetch`

```bash
cat urls.txt | sqx prefetch -c 25 -timeout 10s
sqx prefetch https://example.com https://go.dev
```

SQLite evaluates functions one row at a time, so warm the cache with `prefetch` before running a query over hundreds of URLs. The output columns are `url, final_url, status_code, response_ms, size_bytes, title, error`. Rows come back in input order, and duplicate URLs are fetched once. Use `-c` to set the number of workers (default 10). It also accepts all the [cache flags](#flags).

### `sqx import`

```bash
sqx import -db analytics.db -t users users.csv
cat logs.jsonl | sqx import -db analytics.db -t logs
sqx import -db analytics.db -t countries https://example.com/countries.csv
sqx import -db analytics.db -t users -replace users.csv
```

`import` appends to an existing table and adds any new columns automatically; `-replace` drops the table first. Without `-db`, it writes to `sqx.db` in the current directory. Other flags: `-i <format>`, `-no-header`, and the cache flags (used for URL sources).

### `sqx cache`

```bash
sqx cache stats                     # URL and DNS counts, status breakdown, body size
sqx cache ls                        # every cached URL with status and timestamp
sqx cache query "SELECT url, response_ms FROM http_cache ORDER BY response_ms DESC"
sqx cache clear -older-than 24h     # remove old HTTP and DNS entries
sqx cache clear -all                # wipe everything
```

The cache tables are `http_cache` and `dns_cache`. `clear` refuses to run without `-all` or `-older-than`.

### `sqx tables` / `sqx schema`

```bash
sqx tables -db analytics.db
sqx schema -db analytics.db
```

---

## Flags

### Query flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-db <path>` | `:memory:` | SQLite database for persistent tables |
| `-f <name=path>` | — | Load a file or URL as a table (repeatable). Without `name=`, the table is named after the file. |
| `-t <name>` | `stdin` | Table name for piped stdin |
| `-i <format>` | `auto` | stdin format: `auto`, `csv`, `tsv`, `jsonl`, `json`, `lines` |
| `-o <format>` | `auto` | Output: `auto` (`table` on a terminal, `csv` when piped), `csv`, `tsv`, `table`, `md`, `json`, `jsonl`, `raw` |
| `-save <table>` | — | Save the result into `<table>` in `-db`, replacing it if it exists |
| `-no-header` | `false` | CSV/TSV input has no header row; columns become `c1, c2, …` |
| `-no-output-header` | `false` | Omit the header row in `csv`/`tsv` output |
| `-version` | `false` | Print the version and exit |

### Cache flags

These apply to queries, `prefetch` and `import`.

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-cache-db <path>` | `~/.cache/sqx/http_cache.db` | Cache file (use one per project or in CI) |
| `-cache-ttl <dur>` | `168h` | Max age of cached HTTP and DNS entries; `0s` always refetches |
| `-offline` | `false` | Never use the network; a cache miss returns status `0` / `NULL` |
| `-force` | `false` | Ignore the cache and fetch each URL once more |
| `-timeout <dur>` | `15s` | HTTP request timeout |
| `-ua <string>` | `sqx/<version>` | `User-Agent` header |
| `-max-body-bytes <n>` | `10485760` | Maximum response body size kept per URL |

---

## SQL function reference

### URL (offline)

| Function | Returns | Example |
| :--- | :--- | :--- |
| `url_host(url)` / `url_domain(url)` | Lowercase hostname | `'https://Sub.Example.com:8080/a'` → `sub.example.com` |
| `url_scheme(url)` | Scheme | `'https://example.com'` → `https` |
| `url_path(url)` | Path (`/` if empty) | `'https://example.com/blog?x=1'` → `/blog` |
| `url_query(url, key)` | One query parameter | `('https://x.com/?id=42', 'id')` → `42` |
| `url_ext(url)` | Lowercase file extension | `'https://cdn.com/app.min.js?v=2'` → `.js` |
| `url_norm(url)` | Lowercase host, no fragment, sorted query | `'https://example.com/?b=2&a=1#top'` → `https://example.com/?a=1&b=2` |
| `url_join(base, ref)` | Resolved URL | `('https://a.com/dir/page.html', '../img.png')` → `https://a.com/img.png` |

### HTTP (cached)

| Function | Returns |
| :--- | :--- |
| `http_status(url)` | Status code, or `0` on a network error |
| `http_body(url)` | Response body |
| `http_header(url, name)` | One header (case-insensitive) |
| `http_headers_json(url)` | All headers as a JSON object (names in lowercase) |
| `http_time_ms(url)` | Fetch duration in milliseconds |
| `http_final_url(url)` | URL after redirects |
| `http_size(url)` | Body size in bytes |
| `http_error(url)` | Error message, or `NULL` |

### HTML

Each function accepts **either a URL** (fetched through the cache) **or a raw HTML string**.

| Function | Returns |
| :--- | :--- |
| `html_title(x)` | Text of `<title>` |
| `html_meta(x, key)` | `content` of `<meta name\|property\|http-equiv="key">` |
| `html_text(x)` | Plain text without tags, scripts, styles, `noscript`, comments or the doctype |
| `html_links(x)` | JSON array of `<a href>` links. Links are made absolute when `x` is a URL; `#anchors` and `javascript:` links are skipped. |
| `html_tag_texts(x, tag)` | JSON array of the inner text of every `<tag>` |
| `html_attr(x, tag, attr)` | JSON array of attribute values |

Combine the JSON-array functions with `json_each(...)` to get one row per item.

### DNS, network, regex and hashing

| Function | Returns |
| :--- | :--- |
| `dns_ip(host_or_url)` | First resolved address |
| `dns_ips_json(host_or_url)` | JSON array of every address |
| `dns_txt_json(host_or_url)` | JSON array of TXT records (SPF, DMARC, …) |
| `ip_is_private(ip)` | `1` for loopback, private or link-local addresses, otherwise `0` |
| `text REGEXP pattern` | `1` on a match (Go RE2 syntax; use `(?i)` for case-insensitive) |
| `re_match(text, pattern)` | Same as `REGEXP`, written as a function |
| `re_extract(text, pattern, group)` | One capture group (`0` = whole match) |
| `re_extract_all(text, pattern)` | JSON array of every match |
| `shannon_entropy(text)` | Entropy in bits per character |
| `sha256(text)` | Hex SHA-256 |

---

## Cookbook

Every recipe uses the files in [`data/`](data/). Recipes marked 🌐 need network access.

<details>
<summary><b>Input formats</b>: TSV, JSON, plain lines, headerless CSV, messy headers</summary>

```bash
cat data/metrics.tsv | sqx "SELECT service, endpoint, latency_ms FROM stdin WHERE latency_ms > 300"

cat data/events.jsonl | sqx "
  SELECT id, type, amount, json_extract(meta, '$.device') AS device, tags FROM stdin"

echo '[{"id":1,"zip":"01234"},{"id":2,"active":true}]' | sqx "SELECT * FROM stdin"

cat data/urls.txt | sqx -i lines "SELECT linenum, url FROM stdin"

cat data/headerless.csv | sqx -no-header "SELECT c2 AS city, SUM(c3) AS orders FROM stdin GROUP BY c2"

cat data/messy_headers.csv | sqx "PRAGMA table_info(stdin)"
```

`metrics.tsv` contains an `N/A`, but `latency_ms` is still numeric, so `> 300` compares numbers.
</details>

<details>
<summary><b>Joins and output formats</b></summary>

```bash
cat data/events.jsonl | sqx -t events -f users=data/users.csv "
  SELECT u.country, json_extract(e.meta, '$.device') AS device, COUNT(*) AS n
  FROM events e JOIN users u ON u.id = e.user_id GROUP BY 1, 2"

sqx -f u=data/users.csv -o json "SELECT id, name, plan FROM u"
sqx -f u=data/users.csv -o md   "SELECT id, name, plan FROM u" > output/users.md
sqx -f u=data/users.csv -o raw  "SELECT email FROM u" | xargs -n1 echo mail:

sqx -f u=data/users.csv -o csv "SELECT * FROM u WHERE plan != 'free'" \
  | sqx "SELECT country, COUNT(*) FROM stdin GROUP BY country"
```
</details>

<details>
<summary><b>HTML extraction from stored pages</b>: titles, links, duplicates</summary>

```bash
sqx -f pages=data/pages.jsonl "
  SELECT url_path(url) AS page, html_title(html) AS title,
         html_meta(html, 'description') AS description, html_tag_texts(html, 'h2') AS h2
  FROM pages"

sqx -f pages=data/pages.jsonl "
  SELECT url_path(url) AS page,
         json_array_length(html_attr(html, 'img', 'src')) AS images,
         json_array_length(html_attr(html, 'img', 'alt')) AS with_alt
  FROM pages"

sqx -f pages=data/pages.jsonl "
  SELECT url_host(link.value) AS external_host, COUNT(*) AS links
  FROM pages p, json_each(html_links(p.html)) AS link
  WHERE link.value LIKE 'http%' AND url_host(link.value) != url_host(p.url)
  GROUP BY external_host ORDER BY links DESC"

sqx -f pages=data/pages.jsonl "
  SELECT substr(sha256(html_text(html)), 1, 12) AS content_hash, COUNT(*) AS pages,
         GROUP_CONCAT(url_path(url), ' | ') AS urls
  FROM pages GROUP BY content_hash HAVING pages > 1"
```

Raw HTML has no base URL, so resolve relative links with `url_join(p.url, link.value)`.
</details>

<details>
<summary><b>Secrets, tracking IDs and log analysis</b></summary>

```bash
sqx -f pages=data/pages.jsonl "
  SELECT DISTINCT url_path(p.url) AS page, m.value AS email
  FROM pages p, json_each(re_extract_all(p.html, '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}')) AS m"

sqx -f pages=data/pages.jsonl "
  SELECT url_path(url) AS page,
         re_extract(html, '(UA-[0-9]+-[0-9]+|G-[A-Z0-9]+)', 1) AS tracking_id,
         re_extract(html, 'api_key=([A-Za-z0-9_]+)', 1) AS leaked_key
  FROM pages WHERE html REGEXP 'api_key=|UA-|G-'"

cat data/access_log.csv | sqx "
  SELECT client_ip, url_query(request_url, 'token') AS token,
         ROUND(shannon_entropy(url_query(request_url, 'token')), 2) AS entropy
  FROM stdin
  WHERE NOT ip_is_private(client_ip) AND url_query(request_url, 'token') IS NOT NULL
  ORDER BY entropy DESC"

cat data/access_log.csv | sqx "
  SELECT ts, client_ip, user_agent, request_url FROM stdin
  WHERE request_url REGEXP '(?i)(\.\./|%27|or%201%3d1|/etc/passwd)'
     OR user_agent REGEXP '(?i)sqlmap|nikto|python-requests'"
```
</details>

<details>
<summary>🌐 <b>Website audit</b>: status, redirects, headers, SEO, link graph</summary>

```bash
cat data/urls.txt | sqx prefetch -c 8

cat data/urls.txt | sqx -i lines "
  SELECT url, http_status(url) AS status, http_time_ms(url) AS ms,
         http_final_url(url) AS final_url, http_header(url, 'content-type') AS type,
         html_title(url) AS title, http_error(url) AS error
  FROM stdin ORDER BY status"

cat data/urls.txt | sqx -i lines "
  SELECT url, html_meta(url, 'description') AS description,
         json_array_length(html_tag_texts(url, 'h1')) AS h1_count
  FROM stdin WHERE http_status(url) = 200"

cat data/urls.txt | sqx -i lines "
  SELECT url_host(stdin.url) AS source, url_host(link.value) AS target, COUNT(*) AS links
  FROM stdin, json_each(html_links(stdin.url)) AS link
  WHERE url_host(link.value) != url_host(stdin.url)
  GROUP BY 1, 2 ORDER BY links DESC LIMIT 15"

cat data/urls.txt | sqx -i lines "
  SELECT sha256(html_text(url)) AS hash, COUNT(*) AS pages, GROUP_CONCAT(url, ' | ') AS urls
  FROM stdin WHERE http_status(url) = 200 GROUP BY hash HAVING pages > 1"
```
</details>

<details>
<summary>🌐 <b>DNS and email security</b>: SPF and DMARC</summary>

```bash
cat data/companies.csv | sqx "
  SELECT company, domain, dns_ip(domain) AS ip,
         CASE WHEN dns_txt_json(domain) LIKE '%v=spf1%' THEN 'yes' ELSE 'MISSING' END AS spf,
         CASE WHEN dns_txt_json('_dmarc.' || domain) LIKE '%v=DMARC1%' THEN 'yes' ELSE 'MISSING' END AS dmarc
  FROM stdin"

cat data/urls.txt | sqx -i lines "
  SELECT url_host(url) AS host, dns_ip(url) AS ip, ip_is_private(dns_ip(url)) AS is_private
  FROM stdin GROUP BY host"
```
</details>

<details>
<summary><b>Persistent workspace</b>: import, save, broken-link report</summary>

```bash
sqx import -db output/project.db -t users    data/users.csv
sqx import -db output/project.db -t events   data/events.jsonl
sqx import -db output/project.db -t sitemaps data/sitemaps.csv
sqx import -db output/project.db -t targets  data/targets.jsonl
sqx tables -db output/project.db

sqx -db output/project.db -save big_spenders "
  SELECT u.name, SUM(e.amount) AS total
  FROM users u JOIN events e ON e.user_id = u.id
  GROUP BY u.id HAVING total > 40"
```

🌐 Join sitemaps with live HTTP results and export a Markdown report:

```bash
sqx -db output/project.db -save broken_links "
  SELECT t.category, t.owner, s.loc AS url, http_status(s.loc) AS status, http_error(s.loc) AS error
  FROM sitemaps s JOIN targets t ON url_host(s.loc) = t.domain
  WHERE http_status(s.loc) >= 400 OR http_status(s.loc) = 0"

sqx -db output/project.db -o md "SELECT * FROM broken_links ORDER BY status DESC" > output/broken_links.md
```
</details>

<details>
<summary><b>Creating and updating databases</b>: writes, RETURNING, upserts</summary>

```bash
sqx -db output/shop.db "CREATE TABLE stores (id INTEGER PRIMARY KEY, city TEXT, region TEXT)"
sqx -db output/shop.db "INSERT INTO stores (city) VALUES ('BLR'), ('DEL'), ('MUM')"
sqx -db output/shop.db "INSERT INTO stores (city) VALUES ('HYD') RETURNING id, city"

sqx -db output/shop.db -f orders=data/headerless.csv -no-header "
  UPDATE stores SET region = (SELECT c1 FROM orders WHERE orders.c2 = stores.city LIMIT 1)"

sqx import -db output/shop.db -t users -replace data/users.csv
sqx -db output/shop.db "CREATE UNIQUE INDEX IF NOT EXISTS idx_users_id ON users(id)"
sqx -db output/shop.db -f incoming=data/users.csv "
  INSERT INTO main.users SELECT * FROM incoming WHERE true
  ON CONFLICT(id) DO UPDATE SET plan = excluded.plan, email = excluded.email"
```

SQLite requires the `WHERE true` when an `INSERT … SELECT` has an `ON CONFLICT` clause. A table loaded with `-f` or from stdin hides a `-db` table of the same name, so write `main.<name>` for the persistent one when the names collide.
</details>

---

## Building the binaries yourself

You only need this if you want to build `sqx` from source or publish a release. It requires [Go](https://go.dev/dl/); no C compiler is needed.

**Build for your own machine:**

```bash
git clone https://github.com/sqx-go/sqx-go.github.io.git sqx
cd sqx
CGO_ENABLED=0 go build -o bin/sqx ./sqx
./bin/sqx version
```

**Build for every platform:**

```bash
./scripts/build_releases.sh v1.2.0
```

This writes all 10 binaries and a `SHA256SUMS` file to `releases/`. The version you pass is what `sqx version` prints.

**Publish a release:**

1. Run `./scripts/build_releases.sh vX.Y.Z`.
2. On GitHub, open **Releases → Draft a new release** and create the tag `vX.Y.Z`.
3. Upload every file from `releases/`, including `SHA256SUMS`.
4. Publish. Users can then download from the Releases page, or with `…/releases/latest/download/<file>`.

**Try every example from this README:**

```bash
./scripts/run_examples.sh            # offline examples
./scripts/run_examples.sh --online   # also the HTTP, DNS and crawler examples
```

The script prints each command before running it, and keeps its cache and databases in `output/`.

---

## Documentation website

The full documentation lives at **[sqx-go.github.io](https://sqx-go.github.io/)**. It is generated from the sources in [`site-src/`](site-src/): the home page is written to `index.html` at the repository root, and every other page and asset goes into [`site/`](site/). GitHub Pages serves the `main` branch root:

```bash
python3 scripts/build_site.py      # rebuild index.html and site/ from site-src/
python3 -m http.server 8000        # preview at http://localhost:8000
```

Edit the page content in `site-src/pages/`, page titles, descriptions and navigation in `site-src/pages.json`, and the shared header and footer in `site-src/layout.html`. Don't edit `site/` or `index.html` by hand: the build overwrites them.

---

## Keywords

sql cli, sql command line tool, query csv with sql, query json with sql, sql on csv files, sqlite cli, csv to sqlite, json to sqlite, jsonl, ndjson, tsv, csv to json, json to csv, csv to markdown, join csv files, command line data analysis, data wrangling, etl, unix pipes, web scraping, html parser, url checker, http status checker, broken link checker, seo audit, crawler, dns lookup, spf, dmarc, regex, log analysis, security, osint, golang, static binary, csvkit alternative, q alternative, textql alternative, jq alternative

---

## License

Copyright (C) 2026 Abhishek Sengar

`sqx` is free software: you can redistribute it and/or modify it under the terms of the **GNU General Public License, version 2 or (at your option) any later version** (`GPL-2.0-or-later`), as published by the Free Software Foundation.

It is distributed in the hope that it will be useful, but **without any warranty**, without even the implied warranty of merchantability or fitness for a particular purpose. See the [`LICENSE`](LICENSE) file for the full text.
