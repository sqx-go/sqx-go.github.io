#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
# Copyright (C) 2026 Abhishek Sengar

import html
import json
import re
import shutil
from datetime import date
from pathlib import Path

from unix_tool_pages import load_tool_pages

REPO_ROOT = Path(__file__).resolve().parent.parent
SITE_SOURCE = REPO_ROOT / "site-src"
PAGES_SOURCE = SITE_SOURCE / "pages"
STATIC_SOURCE = SITE_SOURCE / "static"
TOOLS_SOURCE = SITE_SOURCE / "unix-tools.json"
OUTPUT_DIRECTORY = REPO_ROOT
PAGES_DIRECTORY_NAME = "site"
PAGES_DIRECTORY = OUTPUT_DIRECTORY / PAGES_DIRECTORY_NAME
ROOT_FILES = {"", "404.html"}
DOCS_TRAIL = [["Docs", f"{PAGES_DIRECTORY_NAME}/install/"]]
TOOLS_INSERT_AFTER_PATH = f"{PAGES_DIRECTORY_NAME}/functions/"
TOOLS_NAVIGATION_BEFORE_GROUP = "More"

SITE_URL = "https://sqx-go.github.io"
SITE_ROOT = "/"
SITE_NAME = "sqx"
SITE_TAGLINE = "SQL for Unix streams and the web"
REPOSITORY_URL = "https://github.com/sqx-go/sqx-go.github.io"
AUTHOR_NAME = "Abhishek Sengar"
AUTHOR_LINKS = ["https://github.com/monad-one", "https://www.linkedin.com/in/abhishek-sengar"]
HEADING_PATTERN = re.compile(r'<h2 id="([^"]+)">(.*?)</h2>', re.S)
TAG_PATTERN = re.compile(r"<[^>]+>")
BADGE_PATTERN = re.compile(r'<span class="badge">.*?</span>', re.S)


def build_site():
    manifest = json.loads((SITE_SOURCE / "pages.json").read_text())
    layout = (SITE_SOURCE / "layout.html").read_text()
    tool_pages, tools_navigation_group = load_tool_pages(TOOLS_SOURCE)
    pages = insert_after_path(manifest["pages"], TOOLS_INSERT_AFTER_PATH, tool_pages)
    navigation = insert_before_group(manifest["navigation"], TOOLS_NAVIGATION_BEFORE_GROUP, tools_navigation_group)
    doc_pages = [page for page in pages if page["layout"] == "doc"]
    refuse_pages_outside_site_directory(pages)
    reset_pages_directory()
    for page in pages:
        rendered = render_page(layout, page, read_page_body(page), navigation, doc_pages)
        write_page(page["path"], rendered)
    write_sitemap(pages)
    write_robots()
    print(f"built {len(pages)} pages: index.html at the root, the rest in {PAGES_DIRECTORY_NAME}/")


def insert_after_path(pages, anchor_path, new_pages):
    anchor_position = next(index for index, page in enumerate(pages) if page["path"] == anchor_path)
    return pages[:anchor_position + 1] + new_pages + pages[anchor_position + 1:]


def insert_before_group(navigation, anchor_title, new_group):
    anchor_position = next(index for index, group in enumerate(navigation) if group["title"] == anchor_title)
    return navigation[:anchor_position] + [new_group] + navigation[anchor_position:]


def read_page_body(page):
    if "body" in page:
        return page["body"]
    return (PAGES_SOURCE / f"{page['source']}.html").read_text()


def page_section(page):
    if "section" in page:
        return page["section"]
    if page["layout"] == "doc":
        return "docs"
    if page["path"] == f"{PAGES_DIRECTORY_NAME}/about/":
        return "about"
    return page["layout"]


def page_trail(page):
    return page.get("trail", DOCS_TRAIL)


def refuse_pages_outside_site_directory(pages):
    misplaced = [page["path"] for page in pages
                 if page["path"] not in ROOT_FILES and not page["path"].startswith(f"{PAGES_DIRECTORY_NAME}/")]
    if misplaced:
        raise SystemExit(f"pages must live under {PAGES_DIRECTORY_NAME}/: {', '.join(misplaced)}")


def reset_pages_directory():
    if PAGES_DIRECTORY.exists():
        shutil.rmtree(PAGES_DIRECTORY)
    shutil.copytree(STATIC_SOURCE, PAGES_DIRECTORY)
    (OUTPUT_DIRECTORY / ".nojekyll").write_text("")


def render_page(layout, page, body, navigation, doc_pages):
    headings = HEADING_PATTERN.findall(body)
    replacements = {
        "title": html.escape(page["title"]),
        "description": html.escape(page["description"]),
        "keywords": html.escape(", ".join(page.get("keywords", []))),
        "canonical": absolute_url(page["path"]),
        "root": SITE_ROOT,
        "og_type": "website" if page["layout"] == "home" else "article",
        "robots": "noindex" if page.get("noindex") else "index, follow",
        "structured_data": render_structured_data(page),
        "layout": page["layout"],
        "section": page_section(page),
        "sidebar": render_sidebar(navigation, page) if page["layout"] == "doc" else "",
        "breadcrumbs": render_breadcrumbs(page) if page["layout"] == "doc" else "",
        "content": body,
        "pager": render_pager(page, doc_pages) if page["layout"] == "doc" else "",
        "toc": render_toc(headings) if page["layout"] == "doc" and len(headings) > 1 else "",
        "repository": REPOSITORY_URL,
        "year": str(date.today().year),
    }
    rendered = layout
    for key, value in replacements.items():
        rendered = rendered.replace("{{" + key + "}}", value)
    return rendered


def absolute_url(path):
    return f"{SITE_URL}/{path}"


def render_sidebar(navigation, current_page):
    return "".join(render_navigation_group(group, current_page) for group in navigation)


def render_navigation_group(group, current_page):
    items = "".join(render_navigation_item(path, label, current_page) for path, label in group["items"])
    title = html.escape(group["title"])
    if not group.get("collapsible"):
        return f'<div class="nav-group"><h2 class="nav-group-title">{title}</h2><ul>{items}</ul></div>'
    open_attribute = " open" if group.get("section") == page_section(current_page) else ""
    return (
        f'<details class="nav-group is-collapsible"{open_attribute}>'
        f'<summary class="nav-group-title">{title}</summary><ul>{items}</ul></details>'
    )


def render_navigation_item(path, label, current_page):
    current_attribute = ' aria-current="page"' if path == current_page["path"] else ""
    return f'<li><a href="{SITE_ROOT}{path}"{current_attribute}>{html.escape(label)}</a></li>'


def render_breadcrumbs(page):
    ancestors = "".join(
        f'<li><a href="{SITE_ROOT}{path}">{html.escape(label)}</a></li>' for label, path in page_trail(page)
    )
    return (
        '<nav class="breadcrumbs" aria-label="Breadcrumb"><ol>'
        f'<li><a href="{SITE_ROOT}">Home</a></li>{ancestors}'
        f'<li aria-current="page">{html.escape(page["nav_label"])}</li>'
        "</ol></nav>"
    )


def render_pager(page, doc_pages):
    root = SITE_ROOT
    position = next(index for index, candidate in enumerate(doc_pages) if candidate["path"] == page["path"])
    links = []
    if position > 0:
        previous_page = doc_pages[position - 1]
        links.append(pager_link(root, previous_page, "Previous", "is-previous"))
    if position < len(doc_pages) - 1:
        next_page = doc_pages[position + 1]
        links.append(pager_link(root, next_page, "Next", "is-next"))
    return f'<nav class="pager" aria-label="Previous and next pages">{"".join(links)}</nav>'


def pager_link(root, page, direction, modifier):
    return (
        f'<a class="pager-link {modifier}" href="{root}{page["path"]}">'
        f'<span class="pager-direction">{direction}</span>'
        f'<span class="pager-title">{html.escape(page["nav_label"])}</span></a>'
    )


def render_toc(headings):
    items = "".join(
        f'<li><a href="#{anchor}">{html.escape(html.unescape(TAG_PATTERN.sub("", BADGE_PATTERN.sub("", text))).strip())}</a></li>'
        for anchor, text in headings
    )
    return f'<nav class="toc" aria-labelledby="toc-title"><h2 id="toc-title">On this page</h2><ul>{items}</ul></nav>'


def render_structured_data(page):
    graph = [website_schema()]
    if page["layout"] == "home":
        graph.append(software_schema())
        graph.extend(page.get("schema", []))
    elif page["layout"] == "doc":
        graph.append(article_schema(page))
        graph.append(breadcrumb_schema(page))
    elif page["path"] == f"{PAGES_DIRECTORY_NAME}/about/":
        graph.append(profile_schema(page))
    payload = json.dumps({"@context": "https://schema.org", "@graph": graph}, indent=2, ensure_ascii=False)
    safe_payload = payload.replace("</", "<\\/")
    return f'<script type="application/ld+json">\n{safe_payload}\n</script>'


def website_schema():
    return {"@type": "WebSite", "@id": f"{SITE_URL}/#website", "name": SITE_NAME, "url": f"{SITE_URL}/",
            "description": f"{SITE_NAME}: {SITE_TAGLINE}"}


def author_schema():
    return {"@type": "Person", "name": AUTHOR_NAME, "url": f"{SITE_URL}/{PAGES_DIRECTORY_NAME}/about/", "sameAs": AUTHOR_LINKS}


def software_schema():
    return {
        "@type": "SoftwareApplication",
        "@id": f"{SITE_URL}/#software",
        "name": SITE_NAME,
        "url": f"{SITE_URL}/",
        "description": "A single static binary that loads CSV, TSV, JSON, JSON Lines and text into SQLite and "
                       "queries them with SQL, with built-in HTTP fetching, HTML parsing, DNS, regex and hashing "
                       "functions.",
        "applicationCategory": "DeveloperApplication",
        "applicationSubCategory": "Command-line tool",
        "operatingSystem": "macOS, Linux, Windows, FreeBSD",
        "license": "https://spdx.org/licenses/GPL-2.0-or-later.html",
        "codeRepository": REPOSITORY_URL,
        "downloadUrl": f"{REPOSITORY_URL}/releases/latest",
        "programmingLanguage": "Go",
        "offers": {"@type": "Offer", "price": "0", "priceCurrency": "USD"},
        "author": author_schema(),
    }


def article_schema(page):
    return {
        "@type": "TechArticle",
        "headline": page["nav_label"],
        "description": page["description"],
        "url": absolute_url(page["path"]),
        "inLanguage": "en",
        "isPartOf": {"@id": f"{SITE_URL}/#website"},
        "about": {"@id": f"{SITE_URL}/#software"},
        "author": author_schema(),
        "keywords": ", ".join(page.get("keywords", [])),
    }


def breadcrumb_schema(page):
    crumbs = [("Home", f"{SITE_URL}/")]
    crumbs.extend((label, absolute_url(path)) for label, path in page_trail(page))
    crumbs.append((page["nav_label"], absolute_url(page["path"])))
    return {
        "@type": "BreadcrumbList",
        "itemListElement": [
            {"@type": "ListItem", "position": position, "name": name, "item": url}
            for position, (name, url) in enumerate(crumbs, start=1)
        ],
    }


def profile_schema(page):
    person = author_schema()
    person["description"] = ("Builds Android apps, WebGPU experiences and backend systems on Linux and macOS. "
                             "Picks Java, Go or Python to fit the task, tuned for performance and usability, "
                             "and writes tutorials.")
    person["knowsAbout"] = ["Go", "SQL", "SQLite", "Command-line tools", "Web scraping", "Android", "WebGPU",
                            "Java", "Python"]
    return {"@type": "ProfilePage", "name": page["title"], "url": absolute_url(page["path"]), "mainEntity": person}


def write_page(path, rendered):
    target = OUTPUT_DIRECTORY / path / "index.html" if path.endswith("/") or path == "" else OUTPUT_DIRECTORY / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(rendered)


def write_sitemap(pages):
    today = date.today().isoformat()
    entries = "".join(
        f"  <url>\n    <loc>{absolute_url(page['path'])}</loc>\n    <lastmod>{today}</lastmod>\n"
        f"    <priority>{page.get('priority', '0.7')}</priority>\n  </url>\n"
        for page in pages if not page.get("noindex")
    )
    sitemap = ('<?xml version="1.0" encoding="UTF-8"?>\n'
               '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n' + entries + "</urlset>\n")
    (PAGES_DIRECTORY / "sitemap.xml").write_text(sitemap)


def write_robots():
    (OUTPUT_DIRECTORY / "robots.txt").write_text(f"User-agent: *\nAllow: /\n\nSitemap: {SITE_URL}/{PAGES_DIRECTORY_NAME}/sitemap.xml\n")


if __name__ == "__main__":
    build_site()
